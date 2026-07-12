// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/reporter"
	"github.com/Veitangie/sinq/internal/reporter/junit"
	"github.com/Veitangie/sinq/internal/reporter/result"
	"github.com/Veitangie/sinq/internal/reporter/standard"
	"github.com/Veitangie/sinq/internal/runner"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/treewalker"
)

func populateConfigInRuntime(cfg *config.Config) {
	if len(cfg.Paths) == 0 {
		cfg.Paths = append(cfg.Paths, "./")
	}
}

func sinq(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfgParser := config.NewParser()
	cfgParser.Parse(args)
	cfg, errs := cfgParser.Result()

	if len(errs) != 0 {
		fmt.Fprintf(os.Stderr, "Error: Failed to parse flags:\n")
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		}
		return 1
	}

	if cfg.Help {
		fmt.Print(helpPrefix)
		fmt.Println(ponderSinqMeaning())
		fmt.Println(helpSuffix)
		return 0
	}

	if cfg.Version {
		fmt.Print(versionConstPart)
		fmt.Println(ponderSinqMeaning())
		return 0
	}

	populateConfigInRuntime(&cfg)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	walker, err := treewalker.NewTreewalker(cfg, *logger, scenario.ParseRequestBlueprint, scenario.ParseConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to construct treewalker: %s\n", err.Error())
		return 1
	}

	secrets := map[string]any{}
	if cfg.Secrets != "" {
		secrets, err = walker.ParseSecrets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			return 1
		}
	}

	allScenarios := []runner.ScenarioBundle{}
	for _, path := range cfg.Paths {
		fs, err := NewOSWorkspace(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open filetree at %s: %s\n", path, err.Error())
			continue
		}
		res, err := walker.ParseFiletree(ctx, fs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to parse filetree from %s: %s\n", path, err.Error())
			continue
		}
		allScenarios = slices.Grow(allScenarios, len(res))
		for _, scenarioBlueprint := range res {
			allScenarios = append(allScenarios, runner.ScenarioBundle{ScenarioBlueprint: scenarioBlueprint, Workspace: fs})
		}
	}

	if cfg.List {
		for _, scBp := range allScenarios {
			fmt.Fprintf(os.Stdout, "- %s\n", scBp.Config.Name)
			if scBp.Config.Description != "" {
				fmt.Fprintf(os.Stdout, "Description: %s\n", scBp.Config.Description)
			}

			for idx, rqBp := range scBp.Requests {
				fmt.Fprintf(os.Stdout, "  - %d: %s\n", idx+1, rqBp.Filename)
			}
		}
		return 0
	}

	scenarioCount := countTotalScenarios(allScenarios)
	if scenarioCount == 0 {
		fmt.Fprintf(os.Stderr, "Error: No scenarios found\n")
		return 1
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.Workers,
		MaxIdleConnsPerHost:   cfg.Workers,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	rn, err := runner.NewRunner(cfg, ctx, transport, *logger, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to construct runner: %s\n", err.Error())
		return 1
	}

	resultCh, durationCh, errorCh := rn.RunScenarios(ctx, allScenarios, secrets)

	code := handleReporting(cfg, logger, resultCh, durationCh, scenarioCount)

	for err := range errorCh {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
	}

	return code
}

func handleReporting(cfg config.Config, logger *slog.Logger, resultCh <-chan runner.ScenarioResult, durationCh <-chan time.Duration, scenarioCount int) int {

	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(resultReporter)
	if cfg.Out != "" {
		err := report.Register(standard.NewReporter(cfg.Reporter, os.Stderr))
		if err != nil {
			logger.Warn("[sinq] Failed to attach reporter", "error", err)
		}

		file, err := os.OpenFile(cfg.Out, O_CRWRTR, PERM_RW)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to open output file: %s\n", err.Error())
		} else {
			defer func() {
				err := file.Close()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: Failed to close output file: %s\n", err.Error())
				}
			}()

			err = report.Register(createReporter(cfg, file))
			if err != nil {
				logger.Warn("[sinq] Failed to attach reporter", "error", err)
			}
		}
	} else {
		err := report.Register(createReporter(cfg, os.Stdout))
		if err != nil {
			logger.Warn("[sinq] Failed to attach reporter", "error", err)
		}
	}

	err := report.Report(resultCh, durationCh, scenarioCount)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to report results: %s\n", err.Error())
	}

	if resultReporter.Success() {
		return 0
	}

	return 1
}

func createReporter(cfg config.Config, out *os.File) reporter.Reporter {
	switch cfg.Format {
	case "junit":
		return junit.NewReporter(out)
	default:
		reporterCfg := cfg.Reporter
		if reporterCfg.Color == config.Auto {
			fi, _ := out.Stat()
			if fi.Mode()&os.ModeCharDevice == 0 {
				reporterCfg.Color = config.Never
			} else {
				reporterCfg.Color = config.Always
			}
		}
		return standard.NewReporter(reporterCfg, out)
	}
}

func countTotalScenarios(scenarios []runner.ScenarioBundle) int {
	res := len(scenarios)
	for _, scBp := range scenarios {
		mod := 1
		for _, mat := range scBp.Config.EnvMatrix {
			if len(mat) > 0 {
				mod *= len(mat)
			}
		}
		res += mod - 1
	}
	return res
}

func ponderSinqMeaning() string {
	return sinqMeaning[rand.Intn(len(sinqMeaning))]
}

const helpPrefix = "sinq - "

const helpSuffix = `Usage: sinq [flags] [directories...]

A concurrent HTTP functional and integration testing tool.

Flags:
  -w, --workers int      Number of concurrent workers (default 10)
  -s, --safe             Instantiate a new Lua VM per request instead of resetting state
  -i, --insecure         Disable SSL/TLS certificate verification
  -S, --secrets path     Path to the secrets JSON file
  -o, --out path         Path to write the output file (prints to stdout if omitted)
  -L, --log-level string Log level to use: debug, info, warn or error (default "warn")
  -f, --format string    Output format: std or junit (default "std")
  -V, --verbose          Enable verbose reporting (reports each stage duration and timestamps)
  -c, --color string     Terminal colors: always, never, auto (default "auto")
  -l, --list             Parse and list scenarios at specified directories
  -h, --help             Print this help message and exit
  -v, --version          Print the current sinq version and exit`

const versionConstPart = `sinq v1.0.0-rc.4 - `

var sinqMeaning []string = []string{
	"The Spanish Inquisition",
	"Sinq Is Not Quokka",
	"Save Intergalactic Neutrino Quants",
	"A[s]ynchronous Test[in]g Tool[q]it",
	"Sinq Is Now Qombinatorial",
	"Slick, Independent, Novel, Quirky",
}

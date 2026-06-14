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

	if cfg.Reporter.Color == config.Auto {
		fi, _ := os.Stdout.Stat()

		if fi.Mode()&os.ModeCharDevice == 0 {
			cfg.Reporter.Color = config.Never
		} else {
			cfg.Reporter.Color = config.Always
		}
	}

}

func sinq(args []string) int {
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

	var logLevel = slog.LevelInfo
	if cfg.Verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.Workers,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	rn, _ := runner.NewRunner(cfg, ctx, transport, *logger, nil)
	resultCh, durationCh, errorCh := rn.RunScenarios(ctx, allScenarios, secrets)

	stderrReporter := standard.NewReporter(cfg.Reporter, os.Stderr)
	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(stderrReporter, resultReporter)
	if cfg.Out != "" {
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
			var newReporter reporter.Reporter

			switch cfg.Format {
			case "junit":
				newReporter = junit.NewReporter(file)
			default:
				reporterConfig := cfg.Reporter
				reporterConfig.Color = config.Never
				newReporter = standard.NewReporter(reporterConfig, file)
			}

			err = report.Register(newReporter)
			if err != nil {
				logger.Warn("Failed to attach reporter", "error", err)
			}
		}
	}

	err = report.Report(resultCh, durationCh, len(allScenarios))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to report results: %s\n", err.Error())
	}

	for err := range errorCh {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
	}

	code := 1
	if resultReporter.Success() {
		code = 0
	}

	return code
}

func ponderSinqMeaning() string {
	return sinqMeaning[rand.Intn(len(sinqMeaning))]
}

const helpPrefix = "sinq - "

const helpSuffix = `Usage: sinq [flags] [directories...]

A concurrent HTTP functional and integration testing tool.

Flags:
  -w, --workers int    Number of concurrent workers (default 10)
  -s, --safe           Instantiate a new Lua VM per request instead of resetting state
  -i, --insecure       Disable SSL/TLS certificate verification
  -S, --secrets path   Path to the secrets JSON file
  -o, --out path       Path to write the output file (prints to stdout if omitted)
  -f, --format string  Output format: std or junit (default "std")
  -V, --verbose        Enable verbose logging
  -c, --color string   Terminal colors: always, never, auto (default "auto")
  -l, --list           Parse and list scenarios at specified directories
  -h, --help           Print this help message and exit
  -v, --version        Print the current sinq version and exit`

const versionConstPart = `sinq v1.0.0-RC1 - `

var sinqMeaning []string = []string{
	"The Spanish Inquisition",
	"Sinq Is Not Quokka",
	"Save Intergalactic Neutrino Quants",
	"A[s]ynchronous Test[in]g Tool[q]it",
}

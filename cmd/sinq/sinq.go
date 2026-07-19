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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/reporter"
	"github.com/Veitangie/sinq/internal/reporter/junit"
	"github.com/Veitangie/sinq/internal/reporter/result"
	"github.com/Veitangie/sinq/internal/reporter/standard"
	"github.com/Veitangie/sinq/internal/runner"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	"github.com/Veitangie/sinq/internal/treewalker"
)

const sinqLuaPath = "SINQ_LUA_PATH"

func populateConfigInRuntime(cfg *config.Config) {
	if len(cfg.LuaPaths) == 0 {
		if path, ok := os.LookupEnv(sinqLuaPath); ok {
			cfg.LuaPaths = strings.Split(path, ";")
		}
	}

	if len(cfg.Paths) == 0 {
		cfg.Paths = append(cfg.Paths, ".")
	}
}

func sinq(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mainTimer := timer.NewTimer(timer.DefaultClock{})

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
	logger.Debug("[sinq] Initialization complete", "duration", mainTimer.Time())

	walker, err := treewalker.NewTreewalker(cfg, *logger, scenario.ParseRequestBlueprint, scenario.ParseConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to construct treewalker: %s\n", err.Error())
		return 1
	}
	discoveryTimer := timer.NewTimer(timer.DefaultClock{})

	secrets, err := walker.ParseSecrets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		return 1
	}

	allScenarios := []runner.ScenarioBundle{}
	for _, path := range cfg.Paths {
		fs, err := NewOSWorkspace(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open filetree at %s: %s\n", path, err.Error())
			continue
		}
		defer func(path string) {
			err := fs.root.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to close filetree at %s: %s\n", path, err.Error())
			}
		}(path)

		newCtx := context.WithValue(ctx, treewalker.PathCtxKey, path)
		res, err := walker.ParseFiletree(newCtx, fs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to parse filetree from %s: %s\n", path, err.Error())
			continue
		}
		allScenarios = slices.Grow(allScenarios, len(res))
		for _, scenarioBlueprint := range res {
			allScenarios = append(allScenarios, runner.ScenarioBundle{ScenarioBlueprint: scenarioBlueprint, Workspace: fs})
		}
	}
	logger.Debug("[sinq] Discovery complete", "duration", discoveryTimer.Time())

	if cfg.List {
		listScenarios(allScenarios, cfg)
		return 0
	}

	scenarioCount := countTotalScenarios(allScenarios)
	if scenarioCount == 0 {
		fmt.Fprintf(os.Stderr, "Error: No scenarios found\n")
		return 1
	}

	if os.Getenv("CI") != "" {
		if cfg.LogLevel == slog.LevelDebug {
			fmt.Fprintf(os.Stderr, "WARNING: Running in a CI environment with --log-level debug. This risks leaking secrets in CI logs.\n")
		}
		if cfg.DumpOnFailure {
			fmt.Fprintf(os.Stderr, "WARNING: Running in a CI environment with --dump-on-failure. This risks leaking secrets in CI logs if assertions fail.\n")
		}
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

	resultCh, durationCh, errorCh := rn.RunScenarios(ctx, allScenarios, secrets, &mainTimer)

	code := handleReporting(cfg, logger, resultCh, durationCh, errorCh, scenarioCount)

	return code
}

func listScenarios(allScenarios []runner.ScenarioBundle, cfg config.Config) {
	for _, scBp := range allScenarios {
		if cfg.Reporter.Show == config.NoSkip && !cfg.ShouldInclude(scBp.Config.Tags, scBp.Config.Name) {
			continue
		}
		matrixInfo := ""
		comboCount := countOneScenario(scBp)
		if comboCount > 1 {
			matrixInfo = fmt.Sprintf(" (%d matrix combinations)", comboCount)
		}
		fmt.Fprintf(os.Stdout, "- %s%s\n", scBp.Config.Name, matrixInfo)
		if scBp.Config.Description != "" {
			fmt.Fprintf(os.Stdout, "  Description: %s\n", scBp.Config.Description)
		}
		if len(scBp.Config.Tags) != 0 {
			allTags := make([]string, 0, len(scBp.Config.Tags))
			for tag := range scBp.Config.Tags {
				allTags = append(allTags, tag)
			}
			fmt.Fprintf(os.Stdout, "  Tags: [%s]\n", strings.Join(allTags, ", "))
		}

		for idx, rqBp := range scBp.Requests {
			fmt.Fprintf(os.Stdout, "  - %d: %s\n", idx+1, rqBp.Filename)
		}
	}
}

func handleReporting(cfg config.Config, logger *slog.Logger, resultCh <-chan runner.ScenarioResult, durationCh <-chan time.Duration, errorCh <-chan error, scenarioCount int) int {

	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(resultReporter)
	if cfg.Out != "" {
		err := report.Register(standard.NewReporter(cfg.Reporter, os.Stderr))
		if err != nil {
			logger.Warn("[sinq] Failed to attach reporter", "error", err)
		}

		var file *os.File

		if dirPath := filepath.Dir(cfg.Out); dirPath != "" {
			err = os.MkdirAll(dirPath, PERM_RWX)
			if err == nil {
				file, err = os.OpenFile(cfg.Out, O_CRWRTR, PERM_RW)
			}
		}

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

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := report.Report(resultCh, durationCh, scenarioCount)

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to report results: %s\n", err.Error())
		}
	}()

	go func() {
		defer wg.Done()
		for err := range errorCh {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		}
	}()

	wg.Wait()

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
	res := 0
	for _, scBp := range scenarios {
		res += countOneScenario(scBp)
	}
	return res
}

func countOneScenario(scBp runner.ScenarioBundle) int {
	mod := 1
	for _, mat := range scBp.Config.EnvMatrix {
		if len(mat) > 0 {
			mod *= len(mat)
		}
	}
	return mod
}

func ponderSinqMeaning() string {
	return sinqMeaning[rand.Intn(len(sinqMeaning))]
}

const helpPrefix = "sinq - "

const helpSuffix = `Usage: sinq [flags] [directories...]

A concurrent HTTP functional and integration testing tool.

Flags:
  -v, --version          Print the current sinq version and exit
  -h, --help             Print this help message and exit
  -w, --workers int      Number of concurrent workers (default 10)
  -i, --insecure         Disable SSL/TLS certificate verification
  -s, --secret string    Key=value pair overrides for scenario secrets
  -e, --env string       Key=value pair overrides for all scenario environments
  -o, --out path         Path to write the output file (prints to stdout if omitted)
  -L, --log-level string Log level to use: debug, info, warn or error (default "warn")
  -f, --format string    Output format: std or junit (default "std")
  -V, --verbose          Enable verbose reporting (reports each stage duration and timestamps, only affects "std" format)
  -c, --color string     Terminal colors: always, never, auto (default "auto")
  -S, --show string      Which results to show in the output: all, no-skip, failures (default "no-skip")
  -l, --list             Parse and list scenarios at specified directories
  -t, --tag string       Execute only scenarios that have the tag
  -n, --name string      Execute only scenarios which names match the regular expression
  -u, --unrestricted     Load lua "os" and "io" modules for scripts
  --secrets-file string  Path to JSON-formatted secrets file
  --skip-tag string      Do not execute scenarios that have the tag
  --skip-name string     Do not execute scenarios which names match the regular expression
  --plugins string       Paths to lua plugin directory entries, joined with ';'
  --dump-on-failure      Print full request and response data on failed assertion
  --safe                 Instantiate a new Lua VM per request instead of resetting state

For full documentation and examples, visit: https://github.com/Veitangie/sinq/docs
Or read the manual: man 1 sinq`

const versionConstPart = `sinq v1.0.0-rc.8 - `

var sinqMeaning []string = []string{
	"The Spanish Inquisition",
	"Sinq Is Not Quokka",
	"Save Intergalactic Neutrino Quants",
	"A[s]ynchronous Test[in]g Tool[q]it",
	"Sinq Is Now Qombinatorial",
	"Slick, Independent, Novel, Quirky",
	"Stateful Integrated by Network Quality Assurer",
}

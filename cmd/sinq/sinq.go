// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/reporter"
	"veitangie.dev/sinq/internal/reporter/junit"
	"veitangie.dev/sinq/internal/reporter/result"
	"veitangie.dev/sinq/internal/reporter/standard"
	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/sinq/internal/treewalker"
	"veitangie.dev/sinq/internal/ui"
	"veitangie.dev/spinq"
)

const sinqLuaPath = "SINQ_LUA_PATH"

var stdout io.Writer = os.Stdout
var stderr io.Writer = os.Stderr
var isInCi = os.Getenv("NO_COLOR") != "" || os.Getenv("SINQ_NO_COLOR") != "" || os.Getenv("CI") != ""

func populateConfigInRuntime(cfg *config.Config) error {
	if len(cfg.LuaPaths) == 0 {
		if path, ok := os.LookupEnv(sinqLuaPath); ok {
			cfg.LuaPaths = filepath.SplitList(path)
		}
	}

	needToAddCwd := true
	if len(cfg.Paths) == 0 {
		cfg.Paths = append(cfg.Paths, ".")
		needToAddCwd = false
	}

	for idx := range cfg.LuaPaths {
		abs, err := filepath.Abs(cfg.LuaPaths[idx])
		if err != nil {
			return err
		}

		cfg.LuaPaths[idx] = abs
	}

	for _, path := range cfg.Paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		cfg.LuaPaths = append(cfg.LuaPaths, abs)
	}

	if needToAddCwd {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg.LuaPaths = append(cfg.LuaPaths, wd)
	}
	return nil
}

func setupSpinner(inTermErr, inTermOut, color bool, ctx context.Context) (context.CancelFunc, func(*slog.Logger)) {
	if inTermErr && !isInCi {
		pair, err := spinq.WrapPair(ctx, stdout, stderr, ui.GetFrame(color, timer.DefaultClock{}), spinq.Every(100*time.Millisecond))
		if err != nil {
			fmt.Fprintf(stderr, "Failed to set up spinner: %s", err.Error())
			return func() {}, func(*slog.Logger) {}
		}
		if err = pair.Spinny.Start(ctx); err != nil {
			fmt.Fprintf(stderr, "Failed to start spinner: %s", err.Error())
			return func() {}, func(*slog.Logger) {}
		}
		stderr = pair.Spinny
		if inTermOut {
			stdout = pair.Standard
		}
		return pair.Close, func(l *slog.Logger) {
			if l == nil {
				return
			}
			l.Debug("[sinq] Attached spinner logger")
			for err := range pair.Err() {
				l.Debug("[sinq] Spinner encountered error, restarting", "error", err.Error())
				err = pair.Spinny.Start(ctx)
				if err == spinq.ErrClosed {
					l.Debug("[sinq] Got ErrClosed from spinner, detaching")
					return
				}
				if err != nil {
					l.Debug("[sinq] Failed to restart spinner", "error", err.Error())
				}
			}
			l.Debug("[sinq] Spinner was closed, detaching")
		}
	}

	return func() {}, func(*slog.Logger) {}
}

func parseFilePath(ctx context.Context, path string, walker *treewalker.Treewalker) ([]scenario.ScenarioBlueprint, OSRootWorkspace, error) {
	fs := OSRootWorkspace{}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fs, fmt.Errorf("Failed to stat %s: %w\n", path, err)
	}

	wsPath := path
	if !stat.IsDir() {
		wsPath = filepath.Dir(path)
	}

	fs, err = NewOSRootWorkspace(wsPath)
	if err != nil {
		return nil, fs, fmt.Errorf("Failed to open filetree at %s: %s\n", wsPath, err)
	}

	var res []scenario.ScenarioBlueprint
	if stat.IsDir() {
		var err error
		newCtx := context.WithValue(ctx, treewalker.PathCtxKey, path)
		res, err = walker.ParseFiletree(newCtx, fs)
		if err != nil {
			return nil, fs, fmt.Errorf("Error: Failed to parse filetree from %s: %s\n", path, err)
		}
	} else {
		resOne, err := walker.ParseSingleFile(fs, path)
		if err != nil {
			return nil, fs, fmt.Errorf("Error: Failed to parse %s: %s\n", path, err)
		}

		res = []scenario.ScenarioBlueprint{resOne}
	}
	return res, fs, nil
}

func inTermWantColor(fd *os.File, cfg config.Config) (bool, bool) {
	if fd == nil {
		return false, false
	}
	fi, err := fd.Stat()
	inTerm := err == nil && fi.Mode()&os.ModeCharDevice != 0
	return inTerm, cfg.Reporter.Color == config.Always || (cfg.Reporter.Color == config.Auto && !isInCi && inTerm)
}

func sinq(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mainTimer := timer.NewTimer(timer.DefaultClock{})

	cfgParser := config.NewParser(stderr)
	err := cfgParser.Parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to parse flags: %s\n", err.Error())
		return 1
	}
	cfg := cfgParser.Result()

	if cfg.Completion {
		if handleCompletion() {
			return 0
		}

		return 1
	}

	infoPrinter, err := standard.NewInfoPrinter(stdout, version)
	if err != nil {
		return 1
	}

	if cfg.Help {
		err = infoPrinter.PrintHelp()
		if err != nil {
			return 1
		}
		return 0
	}

	if cfg.Version {
		err = infoPrinter.PrintVersion()
		if err != nil {
			return 1
		}
		return 0
	}

	inTermOut, wantColorOut := inTermWantColor(os.Stdout, cfg)
	inTermErr, wantColorErr := inTermWantColor(os.Stderr, cfg)

	attachSpinnerLogger := func(*slog.Logger) {}
	if !cfg.NoSpinner {
		var stopSpinner context.CancelFunc
		stopSpinner, attachSpinnerLogger = setupSpinner(inTermErr, inTermOut, wantColorErr, ctx)
		defer stopSpinner()
	}
	err = infoPrinter.SetWriter(stdout)
	if err != nil {
		return 1
	}

	err = populateConfigInRuntime(&cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to enrich configuration in runtime: %s\n", err.Error())
		fmt.Fprint(stderr, "Will proceed without runtime configuration\n")
		cfg.LuaPaths = []string{}
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Debug("[sinq] Initialization complete", "duration", mainTimer.Time())
	go attachSpinnerLogger(logger)

	walker, err := treewalker.NewTreewalker(cfg, *logger, scenario.ParseRequestBlueprints, scenario.ParseConfig)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct treewalker: %s\n", err.Error())
		return 1
	}
	discoveryTimer := timer.NewTimer(timer.DefaultClock{})

	secrets, err := walker.ParseSecrets()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err.Error())
		return 1
	}

	allScenarios := []runner.ScenarioBundle{}
	for _, path := range cfg.Paths {
		res, fs, err := parseFilePath(ctx, path, walker)
		if err != nil {
			fmt.Fprint(stderr, err.Error())
		}

		defer func(fs OSRootWorkspace) {
			if fs.root == nil {
				return
			}
			err := fs.root.Close()
			if err != nil {
				fmt.Fprintf(stderr, "Failed to close filetree at %s: %s\n", fs.String(), err.Error())
			}
		}(fs)

		if ctx.Err() != nil {
			fmt.Fprint(stderr, "Received interrupt signal, shutting down\n")
			return 1
		}
		allScenarios = slices.Grow(allScenarios, len(res))
		for _, scenarioBlueprint := range res {
			allScenarios = append(allScenarios, runner.ScenarioBundle{ScenarioBlueprint: scenarioBlueprint, Workspace: fs})
		}
	}
	logger.Debug("[sinq] Discovery complete", "duration", discoveryTimer.Time())

	if cfg.List {
		err = infoPrinter.PrintScenarios(allScenarios, cfg, wantColorOut)
		if err != nil {
			logger.Error("[sinq] Failed to list scenarios", "error", err.Error())
			return 1
		}
		return 0
	}

	scenarioCount := countTotalScenarios(allScenarios)
	if scenarioCount == 0 {
		fmt.Fprintf(stderr, "Error: No scenarios found\n")
		return 1
	}

	if os.Getenv("CI") != "" {
		if cfg.LogLevel == slog.LevelDebug {
			fmt.Fprintf(stderr, "WARNING: Running in a CI environment with --log-level debug. This risks leaking secrets in CI logs.\n")
		}
		if cfg.DumpOnFailure {
			fmt.Fprintf(stderr, "WARNING: Running in a CI environment with --dump-on-failure. This risks leaking secrets in CI logs if assertions fail.\n")
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
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}

	rn, err := runner.NewRunner(cfg, ctx, transport, *logger, timer.DefaultClock{}, OSWorkspace{})
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct runner: %s\n", err.Error())
		return 1
	}

	resultCh, durationCh, errorCh := rn.RunScenarios(ctx, allScenarios, secrets, &mainTimer)

	code := handleReporting(cfg, logger, resultCh, durationCh, errorCh, scenarioCount, wantColorErr, wantColorOut)

	return code
}

func handleReporting(
	cfg config.Config,
	logger *slog.Logger,
	resultCh <-chan runner.ScenarioResult,
	durationCh <-chan time.Duration,
	errorCh <-chan error,
	scenarioCount int,
	wantColorErr, wantColorOut bool,
) int {

	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(resultReporter)
	if cfg.Out != "" {
		err := report.Register(createReporter(cfg, stderr, wantColorErr))
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
			fmt.Fprintf(stderr, "Error: Failed to open output file: %s\n", err.Error())
		} else {
			defer func() {
				err := file.Close()
				if err != nil {
					fmt.Fprintf(stderr, "Error: Failed to close output file: %s\n", err.Error())
				}
			}()

			err = report.Register(createReporter(cfg, file, false))
			if err != nil {
				logger.Warn("[sinq] Failed to attach reporter", "error", err)
			}
		}
	} else {
		err := report.Register(createReporter(cfg, stdout, wantColorOut))
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
			fmt.Fprintf(stderr, "Error: Failed to report results: %s\n", err.Error())
		}
	}()

	go func() {
		defer wg.Done()
		for err := range errorCh {
			fmt.Fprintf(stderr, "Error: %s\n", err.Error())
		}
	}()

	wg.Wait()

	if resultReporter.Success() {
		return 0
	}

	return 1
}

func createReporter(cfg config.Config, out io.Writer, wantColor bool) reporter.Reporter {
	switch cfg.Format {
	case "junit":
		return junit.NewReporter(out)
	default:
		reporterCfg := cfg.Reporter

		if reporterCfg.Color == config.Auto {
			if wantColor {
				reporterCfg.Color = config.Always
			} else {
				reporterCfg.Color = config.Never
			}
		}
		return standard.NewReporter(reporterCfg, out)
	}
}

func countTotalScenarios(scenarios []runner.ScenarioBundle) int {
	res := 0
	for _, scBp := range scenarios {
		res += scBp.CountVariants()
	}
	return res
}

//go:embed completions/sinq.ps1
var ps1Comp string

//go:embed completions/sinq.bash
var bashComp string

//go:embed completions/_sinq
var zshComp string

//go:embed completions/sinq.fish
var fishComp string

func detectActiveShell() (string, error) {
	ppid := os.Getppid()
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", ppid))
	if err != nil {
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ppid))
		if err != nil {
			return "", err
		}
		return string(cmdline), nil
	}

	return filepath.Base(exePath), nil
}

func handleCompletion() bool {
	if runtime.GOOS == "windows" {
		fmt.Fprint(stdout, ps1Comp)
		return true
	}

	shell, err := detectActiveShell()
	if err != nil {
		shell = filepath.Base(os.Getenv("SHELL"))
	}

	switch strings.ToLower(shell) {
	case "bash":
		fmt.Fprint(stdout, bashComp)
	case "zsh":
		fmt.Fprint(stdout, zshComp)
	case "fish":
		fmt.Fprint(stdout, fishComp)
	default:
		return false
	}
	return true
}

var version string = "dev"

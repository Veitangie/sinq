// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/reporter"
	"veitangie.dev/sinq/internal/reporter/junit"
	"veitangie.dev/sinq/internal/reporter/result"
	"veitangie.dev/sinq/internal/reporter/spinner"
	"veitangie.dev/sinq/internal/reporter/standard"
	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/sinq/internal/treewalker"
	"veitangie.dev/sinq/internal/ui"
	"veitangie.dev/spinq"
)

const (
	codeSuccess int = iota
	codeRunFailure
	codeRunError
	codeSetupError
	codeAborted int = 130
)

const sinqLuaPath = "SINQ_LUA_PATH"

var stdout io.Writer = os.Stdout
var stderr spinq.Writer = spinq.WriterPassthrough{Writer: os.Stderr}
var isInCi = os.Getenv("NO_COLOR") != "" || os.Getenv("SINQ_NO_COLOR") != "" || os.Getenv("CI") != ""

func populateConfigInRuntime(cfg *config.Config) (string, error) {
	if len(cfg.LuaPaths) == 0 {
		if path, ok := os.LookupEnv(sinqLuaPath); ok {
			cfg.LuaPaths = filepath.SplitList(path)
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	needToAddCwd := true
	if len(cfg.Paths) == 0 {
		cfg.Paths = append(cfg.Paths, ".")
		needToAddCwd = false
	}

	for idx := range cfg.LuaPaths {
		abs, err := filepath.Abs(cfg.LuaPaths[idx])
		if err != nil {
			return wd, err
		}

		cfg.LuaPaths[idx] = abs
	}

	for _, path := range cfg.Paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return wd, err
		}

		cfg.LuaPaths = append(cfg.LuaPaths, abs)
	}

	if needToAddCwd {
		cfg.LuaPaths = append(cfg.LuaPaths, wd)
	}
	return wd, nil
}

func setupSpinner(color bool, ctx context.Context) (context.CancelFunc, func(*slog.Logger)) {
	if !isInCi {
		pair, err := spinq.WrapOS(ctx, ui.GetSimpleFrame(color, timer.DefaultClock{}, "Starting"), spinq.Every(100*time.Millisecond), spinq.WrapWithDefaultResizeDetection(ctx))
		if err != nil {
			fmt.Fprintf(stderr, "Failed to set up spinner: %s", err.Error())
			return func() {}, func(*slog.Logger) {}
		}

		if pair.Spinner.IsLive() {
			fmt.Fprint(stderr, spinq.HideCursor)
		}
		if err = pair.Spinner.Start(ctx); err != nil {
			fmt.Fprintf(stderr, "%sFailed to start spinner: %s", spinq.ShowCursor, err.Error())
			return func() {}, func(*slog.Logger) {}
		}
		stderr = pair.Spinner
		stdout = pair.Standard
		return func() {
				stderr.Close()
				if stderr.IsLive() {
					fmt.Fprint(pair.Spinner, spinq.ShowCursor)
				}
			}, func(l *slog.Logger) {
				if l == nil {
					return
				}
				l.Debug("[sinq] Attached spinner logger")
				for err := range pair.Spinner.Err() {
					l.Debug("[sinq] Spinner encountered error, restarting", "error", err.Error())
					err = pair.Spinner.Start(ctx)
					if errors.Is(err, spinq.ErrClosed) {
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

func indentedError(err error) string {
	lines := strings.Split(strings.TrimRight(err.Error(), "\n"), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func parseFilePath(ctx context.Context, cwd, path string, walker *treewalker.Treewalker) ([]scenario.ScenarioBlueprint, OSRootWorkspace, error) {
	fs := OSRootWorkspace{}
	stat, err := os.Stat(path)
	if err != nil {
		if pathErr, ok := errors.AsType[*iofs.PathError](err); ok {
			err = pathErr.Err
		}
		return nil, fs, fmt.Errorf("%s: %w", path, err)
	}

	wsPath := path
	if !stat.IsDir() {
		wsPath = filepath.Dir(path)
	}

	absWsPath, err := filepath.Abs(wsPath)
	if err != nil {
		return nil, fs, fmt.Errorf("%s: Failed to get the absolute path: %w", wsPath, err)
	}

	rootName, err := filepath.Rel(cwd, absWsPath)
	if err != nil {
		rootName = wsPath
	}

	fs, err = NewOSRootWorkspace(wsPath)
	if err != nil {
		return nil, fs, fmt.Errorf("%s: Failed to open the folder: %w", wsPath, err)
	}

	var res []scenario.ScenarioBlueprint

	if stat.IsDir() {
		var err error
		newCtx := context.WithValue(ctx, treewalker.PathCtxKey, path)
		res, err = walker.ParseFiletree(newCtx, fs, rootName)
		if err != nil {
			return nil, fs, err
		}
	} else {
		resOne, err := walker.ParseSingleFile(fs, path, rootName)
		if err != nil {
			return nil, fs, err
		}

		res = []scenario.ScenarioBlueprint{resOne}
	}
	return res, fs, nil
}

func wantColor(fd *os.File, cfg config.Config) bool {
	if fd == nil {
		return false
	}
	return cfg.Reporter.Color == config.Always || (cfg.Reporter.Color == config.Auto && !isInCi && (isatty.IsTerminal(fd.Fd()) || isatty.IsCygwinTerminal(fd.Fd())))
}

func sinq(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mainTimer := timer.NewTimer(timer.DefaultClock{})

	cfgParser := config.NewParser(stderr)
	err := cfgParser.Parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to parse flags:\n%s\n", err.Error())
		return codeSetupError
	}
	cfg := cfgParser.Result()

	if cfg.Completion {
		if handleCompletion() {
			return codeSuccess
		}

		return codeSetupError
	}

	infoPrinter, err := standard.NewInfoPrinter(stdout, version)
	if err != nil {
		return codeSetupError
	}

	if cfg.Help {
		err = infoPrinter.PrintHelp()
		if err != nil {
			return codeSetupError
		}
		return codeSuccess
	}

	if cfg.Version {
		err = infoPrinter.PrintVersion()
		if err != nil {
			return codeSetupError
		}
		return codeSuccess
	}

	wantColorOut := wantColor(os.Stdout, cfg)
	wantColorErr := wantColor(os.Stderr, cfg)

	attachSpinnerLogger := func(*slog.Logger) {}
	if !cfg.NoSpinner {
		var stopSpinner context.CancelFunc
		stopSpinner, attachSpinnerLogger = setupSpinner(wantColorErr, ctx)
		defer stopSpinner()
	}
	err = infoPrinter.SetWriter(stdout)
	if err != nil {
		return codeSetupError
	}

	cwd, err := populateConfigInRuntime(&cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to enrich configuration in runtime:\n%s\n", err.Error())
		fmt.Fprint(stderr, "Will proceed without runtime configuration\n")
		cfg.LuaPaths = []string{}
	}

	if cwd == "" {
		fmt.Fprint(stderr, "Error: Failed to find current working directory\n")
		fmt.Fprint(stderr, "Will proceed without current working directory\n")
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	go attachSpinnerLogger(logger)

	eb, err := event.NewBus(cfg.Workers*2, logger)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct event bus:\n%s\n", err.Error())
		return codeSetupError
	}
	eb.Start()
	defer eb.Close()
	logger.Debug("[sinq] Initialization complete", "duration", mainTimer.Time())

	walker, err := treewalker.NewTreewalker(cfg, eb, scenario.ParseStageBlueprints, scenario.ParseConfig, cwd)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct treewalker:\n%s\n", err.Error())
		return codeSetupError
	}
	discoveryTimer := timer.NewTimer(timer.DefaultClock{})

	secrets, err := walker.ParseSecrets()
	if err != nil {
		fmt.Fprintf(stderr, "Error:\n%s\n", err.Error())
		return codeSetupError
	}

	scenarioCount := 0
	willRunCount := 0
	allScenarios := []runner.ScenarioBundle{}
	var treewalkerErr error
	for _, path := range cfg.Paths {
		res, fs, err := parseFilePath(ctx, cwd, path, walker)
		if err != nil {
			treewalkerErr = errors.Join(treewalkerErr, err)
		}

		defer func(fs OSRootWorkspace) {
			if fs.root == nil {
				return
			}
			err := fs.root.Close()
			if err != nil {
				fmt.Fprintf(stderr, "Failed to close filetree at %s:\n%s\n", fs.String(), err.Error())
			}
		}(fs)

		if ctx.Err() != nil {
			fmt.Fprint(stderr, "Received interrupt signal, shutting down\n")
			return codeAborted
		}

		allScenarios = slices.Grow(allScenarios, len(res))
		for _, scenarioBlueprint := range res {
			count := cfg.Count
			variantCount := scenarioBlueprint.CountVariants()
			if !cfg.ShouldInclude(scenarioBlueprint.Config.Tags, scenarioBlueprint.Config.Name) {
				count = 0
				scenarioCount += variantCount
			}
			variantCount *= int(count)
			scenarioCount += variantCount
			willRunCount += variantCount
			allScenarios = append(allScenarios, runner.ScenarioBundle{ScenarioBlueprint: scenarioBlueprint, Workspace: fs, Count: uint(count)})
		}
	}
	logger.Debug("[sinq] Discovery complete", "duration", discoveryTimer.Time())

	if treewalkerErr != nil {
		fmt.Fprintf(stderr, "Error: Failed to parse scenarios:\n%s\n", indentedError(treewalkerErr))
		return codeSetupError
	}

	if cfg.List {
		err = infoPrinter.PrintScenarios(allScenarios, cfg, wantColorOut)
		if err != nil {
			fmt.Fprintf(stderr, "Error: Failed to list scenarios:\n%s\n", indentedError(err))
			return codeSetupError
		}
		return codeSuccess
	}

	if willRunCount == 0 {
		fmt.Fprint(stderr, "Error: No scenarios to run. Did you mistype filters?\n")
		return codeSetupError
	}

	if os.Getenv("CI") != "" {
		if cfg.LogLevel == slog.LevelDebug {
			fmt.Fprintf(stderr, "WARNING: Running in a CI environment with --log-level debug. This risks leaking secrets in CI logs.\n")
		}
		if cfg.DumpOnFailure {
			fmt.Fprintf(stderr, "WARNING: Running in a CI environment with --dump-on-failure. This risks leaking secrets in CI logs if stages fail.\n")
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

	rn, err := runner.NewRunner(cfg, ctx, transport, eb, timer.DefaultClock{}, OSWorkspace{})
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct runner:\n%s\n", err.Error())
		return codeSetupError
	}

	agg := aggregator.NewAggregator()
	resultCh, durationCh := agg.AggregateOver(eb.RegisterMC(cfg.Workers * 2))

	codeCh := make(chan int)
	go func() {
		code := handleReporting(cfg, logger, resultCh, durationCh, scenarioCount, willRunCount, wantColorErr, wantColorOut)
		codeCh <- code
		close(codeCh)
	}()

	rn.RunScenarios(ctx, allScenarios, secrets, &mainTimer)

	return <-codeCh
}

func handleReporting(
	cfg config.Config,
	logger *slog.Logger,
	resultCh <-chan aggregator.ScenarioResult,
	durationCh <-chan time.Duration,
	scenarioCount int,
	willRunCount int,
	wantColorErr, wantColorOut bool,
) int {

	res := 0
	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(resultReporter)

	spinnerReporter, err := spinner.NewSpinnerReporter(stderr, timer.DefaultClock{}, willRunCount, wantColorErr)
	if err == nil {
		err = report.Register(spinnerReporter)
	}

	if err != nil {
		logger.Debug("[sinq] Failed to attach spinner reporter", "error", err)
	}

	if cfg.Out != "" {
		cloneCfg := cfg
		cloneCfg.Format = "std"
		err := report.Register(createReporter(cloneCfg, stderr, wantColorErr))
		if err != nil {
			logger.Debug("[sinq] Failed to attach reporter", "error", err)
		}

		var file *os.File

		if dirPath := filepath.Dir(cfg.Out); dirPath != "" {
			err = os.MkdirAll(dirPath, PERM_RWX)
			if err == nil {
				file, err = os.OpenFile(cfg.Out, O_CRWRTR, PERM_RW)
			}
		}

		if err != nil {
			res = codeSetupError
			fmt.Fprintf(stderr, "Error: Failed to open output file:\n%s\n", err.Error())
		} else {
			defer func() {
				err := file.Close()
				if err != nil {
					fmt.Fprintf(stderr, "Error: Failed to close output file:\n%s\n", err.Error())
				}
			}()

			err = report.Register(createReporter(cfg, file, false))
			if err != nil {
				logger.Debug("[sinq] Failed to attach reporter", "error", err)
			}
		}
	} else {
		err := report.Register(createReporter(cfg, stdout, wantColorOut))
		if err != nil {
			logger.Debug("[sinq] Failed to attach reporter", "error", err)
		}
	}

	sig := make(chan error, 1)
	go func() {
		defer close(sig)
		sig <- report.Report(resultCh, durationCh, scenarioCount)
	}()

	err = <-sig
	if err != nil {
		logger.Debug("[sinq] Failed to report", "error", err)
		fmt.Fprintf(stderr, "Error: Failed to report results:\n%s\n", err.Error())
		res = codeSetupError
	}

	if res != 0 {
		return res
	}

	switch resultReporter.Status() {
	case event.Failure:
		res = codeRunFailure
	case event.Aborted:
		res = codeAborted
	case event.Error:
		res = codeRunError
	default:
		res = codeSuccess
	}
	return res
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

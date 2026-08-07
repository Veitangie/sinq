// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
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

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/reporter"
	"github.com/Veitangie/sinq/internal/reporter/junit"
	"github.com/Veitangie/sinq/internal/reporter/result"
	"github.com/Veitangie/sinq/internal/reporter/standard"
	"github.com/Veitangie/sinq/internal/runner"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	"github.com/Veitangie/sinq/internal/treewalker"
	"github.com/Veitangie/sinq/internal/ui"
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

	if len(cfg.Paths) == 0 {
		cfg.Paths = append(cfg.Paths, ".")
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

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg.LuaPaths = append(cfg.LuaPaths, wd)
	return nil
}

func setupSpinner(inTermErr, inTermOut bool, ctx context.Context) context.CancelFunc {
	if inTermErr && !isInCi {
		uiWriter, spinnerWriter := ui.MakePair(stdout, stderr)
		ctxWithCancel, cancel := context.WithCancel(ctx)
		spinnerWriter.StartSpinner(ctxWithCancel, timer.DefaultClock{})
		stderr = spinnerWriter
		if inTermOut {
			stdout = uiWriter
		}
		return func() {
			cancel()
			spinnerWriter.Close()
		}
	}

	return func() {}
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

func sinq(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mainTimer := timer.NewTimer(timer.DefaultClock{})

	cfgParser := config.NewParser()
	cfgParser.Parse(args)
	cfg, errs := cfgParser.Result()

	if len(errs) != 0 {
		fmt.Fprintf(stderr, "Error: Failed to parse flags:\n")
		for _, err := range errs {
			fmt.Fprintf(stderr, "%s\n", err.Error())
		}
		return 1
	}

	if cfg.Completion {
		if handleCompletion() {
			return 0
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

	fi, err := os.Stdout.Stat()
	inTermOut := err == nil && fi.Mode()&os.ModeCharDevice != 0

	fi, err = os.Stderr.Stat()
	inTermErr := err == nil && fi.Mode()&os.ModeCharDevice != 0

	if !cfg.NoSpinner {
		stopSpinner := setupSpinner(inTermErr, inTermOut, ctx)
		defer stopSpinner()
	}

	err = populateConfigInRuntime(&cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to enrich configuration in runtime: %s\n", err.Error())
		fmt.Fprint(stderr, "Will proceed without runtime configuration\n")
		cfg.LuaPaths = []string{}
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Debug("[sinq] Initialization complete", "duration", mainTimer.Time())

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
	}

	rn, err := runner.NewRunner(cfg, ctx, transport, *logger, timer.DefaultClock{}, OSWorkspace{})
	if err != nil {
		fmt.Fprintf(stderr, "Error: Failed to construct runner: %s\n", err.Error())
		return 1
	}

	resultCh, durationCh, errorCh := rn.RunScenarios(ctx, allScenarios, secrets, &mainTimer)

	code := handleReporting(cfg, logger, resultCh, durationCh, errorCh, scenarioCount, inTermErr, inTermOut)

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
		fmt.Fprintf(stdout, "- %s%s\n", scBp.Config.Name, matrixInfo)
		if scBp.Config.Description != "" {
			fmt.Fprintf(stdout, "  Description: %s\n", scBp.Config.Description)
		}
		if len(scBp.Config.Tags) != 0 {
			allTags := make([]string, 0, len(scBp.Config.Tags))
			for tag := range scBp.Config.Tags {
				allTags = append(allTags, tag)
			}
			fmt.Fprintf(stdout, "  Tags: [%s]\n", strings.Join(allTags, ", "))
		}

		for idx, rqBp := range scBp.Requests {
			maybeName := rqBp.Filename
			if rqBp.Name != "" {
				maybeName = rqBp.Name
			}
			fmt.Fprintf(stdout, "  - %d: %s\n", idx+1, maybeName)
		}
	}
}

func handleReporting(
	cfg config.Config,
	logger *slog.Logger,
	resultCh <-chan runner.ScenarioResult,
	durationCh <-chan time.Duration,
	errorCh <-chan error,
	scenarioCount int,
	inTermErr, inTermOut bool,
) int {

	resultReporter := result.NewResultReporter()

	report := reporter.NewPool(resultReporter)
	if cfg.Out != "" {
		err := report.Register(createReporter(cfg, stderr, inTermErr))
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
		err := report.Register(createReporter(cfg, stdout, inTermOut))
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

func createReporter(cfg config.Config, out io.Writer, inTerm bool) reporter.Reporter {
	switch cfg.Format {
	case "junit":
		return junit.NewReporter(out)
	default:
		reporterCfg := cfg.Reporter

		if reporterCfg.Color == config.Auto {
			if !inTerm || isInCi {
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

func ponderSinqMeaning() string {
	return sinqMeaning[rand.Intn(len(sinqMeaning))]
}

const helpPrefix = "sinq - "

const helpSuffix = `Usage: sinq [flags] [directories...]

A concurrent HTTP functional and integration testing tool.

Flags:
  -v, --version           Print the current sinq version and exit
  -h, --help              Print this help message and exit
  -w, --workers int       Number of concurrent workers (default 10)
  -i, --insecure          Disable SSL/TLS certificate verification
  -s, --secret string     Key=value pair overrides for scenario secrets
  -e, --env string        Key=value pair overrides for all scenario environments
  -o, --out path          Path to write the output file (prints to stdout if omitted)
  -L, --log-level string  Log level to use: debug, info, warn or error (default "warn")
  -f, --format string     Output format: std or junit (default "std")
  -V, --verbose           Enable verbose reporting (reports each stage duration, only affects "std" format)
  -c, --color string      Terminal colors: always, never, auto (default "auto")
  -S, --show string       Which results to show in the output: all, no-skip, failed (default "no-skip")
  -l, --list              Parse and list scenarios at specified directories
  -t, --tag string        Execute only scenarios that have the tag
  -n, --name string       Execute only scenarios which names match the regular expression
  -u, --unrestricted      Load lua "os" and "io" modules for scripts
  -p, --print             Capture lua output and show it in the report
  --secrets-file string   Path to JSON-formatted secrets file
  --skip-tag string       Do not execute scenarios that have the tag
  --skip-name string      Do not execute scenarios which names match the regular expression
  --plugins string        Paths to lua plugin directory entries, joined with ':' on Linux and MacOS, ';' on Windows
  --max-cache-size string Global maximum response size for cached requests, default 5MB
  --cache-timeout string  Global timeout for the cached requests, default 10s
  --dump-on-failure       Print full request and response data on failed assertion
  --no-spinner            Disable spinner animation

For full documentation and examples, visit: https://github.com/Veitangie/sinq/docs
Or read the manual: man 1 sinq`

var versionConstPart = "sinq dev - "

var sinqMeaning []string = []string{
	"The Spanish Inquisition",
	"Sinq Is Not Quokka",
	"Save Intergalactic Neutrino Quants",
	"A[s]ynchronous Test[in]g Tool[q]it",
	"Sinq Is Now Qombinatorial",
	"Slick, Independent, Novel, Quirky",
	"Stateful Integrated by Network Quality Assurer",
	"Stealth Interpreter, Normal Querier",
	"[S]top searching for mean[inq]",
	"[Sin]q is on Arch Linu[q]s",
	"[S]leepless [ni]ghts in [Q]azaqstan",
	"[S]cenario-based [i]nstantiator of [n]on-deterministic se[q]uences",
}

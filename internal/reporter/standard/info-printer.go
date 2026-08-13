// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/spinq"
)

type InfoPrinter struct {
	writer  io.Writer
	version string
}

func NewInfoPrinter(writer io.Writer, version string) (InfoPrinter, error) {
	res := InfoPrinter{writer, version}
	if writer == nil {
		return res, errors.New("Unable to create info reporter: writer is nil")
	}

	return res, nil
}

func (p InfoPrinter) PrintVersion() error {
	_, err := fmt.Fprintf(p.writer, "sinq %s - ", p.version)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.writer, ponderSinqMeaning())
	if err != nil {
		return err
	}
	return nil
}

func (p InfoPrinter) PrintHelp() error {
	_, err := fmt.Fprint(p.writer, helpPrefix)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.writer, ponderSinqMeaning())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.writer, helpSuffix)
	if err != nil {
		return err
	}
	return nil
}

func (p *InfoPrinter) SetWriter(writer io.Writer) error {
	if writer == nil {
		return errors.New("Unable to set nil writer")
	}
	p.writer = writer
	return nil
}

func (p InfoPrinter) PrintScenarios(allScenarios []runner.ScenarioBundle, cfg config.Config, wantColor bool) error {
	cyan := ""
	reset := ""
	if wantColor {
		cyan = spinq.Cyan
		reset = spinq.ResetColor
	}
	skipHighlight := cyan + "┃" + reset
	middleHighlight := cyan + "┣━" + reset
	endHighlight := cyan + "┗━" + reset
	totalCount := 0
	var err error

	for _, scBp := range allScenarios {
		if cfg.Reporter.Show == config.NoSkip && !cfg.ShouldInclude(scBp.Config.Tags, scBp.Config.Name) {
			continue
		}
		matrixInfo := ""
		comboCount := scBp.CountVariants()
		totalCount += comboCount
		if comboCount > 1 {
			matrixInfo = fmt.Sprintf(" (%d matrix combinations)", comboCount)
		}

		_, err = fmt.Fprintf(p.writer, " %s%s\n", scBp.Config.Name, matrixInfo)
		if err != nil {
			return err
		}
		if scBp.Config.Description != "" {
			_, err = fmt.Fprintf(p.writer, " %s Description: %s\n", skipHighlight, scBp.Config.Description)
			if err != nil {
				return err
			}
		}
		if len(scBp.Config.Tags) != 0 {
			allTags := make([]string, 0, len(scBp.Config.Tags))
			for tag := range scBp.Config.Tags {
				allTags = append(allTags, tag)
			}
			_, err = fmt.Fprintf(p.writer, " %s Tags: [%s]\n", skipHighlight, strings.Join(allTags, ", "))
			if err != nil {
				return err
			}
		}

		for idx, rqBp := range scBp.Requests {
			maybeName := rqBp.Filename
			if rqBp.Name != "" {
				maybeName = rqBp.Name
			}
			if idx == len(scBp.Requests)-1 {
				_, err = fmt.Fprintf(p.writer, " %s%s\n", endHighlight, maybeName)
				if err != nil {
					return err
				}
				continue
			}
			_, err = fmt.Fprintf(p.writer, " %s%s\n", middleHighlight, maybeName)
			if err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(p.writer, " Total: %d\n", totalCount)
	return err
}

func ponderSinqMeaning() string {
	return sinqMeaning[rand.IntN(len(sinqMeaning))]
}

const helpPrefix = "sinq - "

const helpSuffix = `Usage: sinq [flags] [directories...]

A concurrent HTTP functional and integration testing tool.

Flags:
  -v, --version           Print the current sinq version and exit
  -h, --help              Print this help message and exit
  -i, --insecure          Disable SSL/TLS certificate verification
  -V, --verbose           Enable verbose reporting (reports each stage duration, only affects "std" format)
  -l, --list              Parse and list scenarios at specified directories
  -u, --unrestricted      Load lua "os" and "io" modules for scripts
  -p, --print             Capture lua output and show it in the report
  -w, --workers int       Number of concurrent workers (default 10)
  -c, --count int         Number of launches for every scenario
  -s, --secret string     Key=value pair overrides for scenario secrets
  -e, --env string        Key=value pair overrides for all scenario environments
  -o, --out path          Path to write the output file (prints to stdout if omitted)
  -L, --log-level string  Log level to use: debug, info, warn or error (default "warn")
  -f, --format string     Output format: std or junit (default "std")
  -C, --color string      Terminal colors: always, never, auto (default "auto")
  -S, --show string       Which results to show in the output: all, no-skip, failed, none (default "no-skip")
  -t, --tag string        Execute only scenarios that have at least one of passed tags
  -n, --name string       Execute only scenarios which names match at least one of passed regular expressions
  --no-spinner            Disable spinner animation
  --dump-on-failure       Print full request and response data on failed assertion
  --secrets-file string   Path to JSON-formatted secrets file
  --no-tag string         Do not execute scenarios that have the tag
  --no-name string        Do not execute scenarios which names match the regular expression
  --plugins string        Paths to lua plugin directory entries, joined with ':' on Linux and MacOS, ';' on Windows
  --max-cache-size string Global maximum response size for cached requests, default 5MB
  --cache-timeout string  Global timeout for the cached requests, default 10s

For full documentation, visit https://github.com/Veitangie/sinq/docs or https://sinq.veitangie.dev
Or read the manual: man 1 sinq`

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
	"[S]lowly [in]ching towards going publi[q]",
}

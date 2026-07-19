// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type parserState int

const (
	Flags parserState = iota
	Positional
	Finalized
)

var outFormats map[string]bool = map[string]bool{
	"junit": true,
	"std":   true,
}

var longToShort map[string]string = map[string]string{
	"--workers":      "-w",
	"--insecure":     "-i",
	"--secret":       "-s",
	"--env":          "-e",
	"--out":          "-o",
	"--log-level":    "-L",
	"--format":       "-f",
	"--verbose":      "-V",
	"--color":        "-c",
	"--help":         "-h",
	"--version":      "-v",
	"--list":         "-l",
	"--tag":          "-t",
	"--name":         "-n",
	"--show":         "-S",
	"--unrestricted": "-u",
}

type Parser struct {
	result            Config
	state             parserState
	accumulatedErrors []error
	curIdx            int
	currentFlags      []string
}

func NewParser() Parser {
	return Parser{result: SaneDefaults()}
}

func (p *Parser) Parse(flags []string) {
	p.currentFlags = slices.Clone(flags)
	p.curIdx = 0

	for p.curIdx < len(p.currentFlags) {
		p.parsePositional()
		p.getNext()
	}

	p.currentFlags = nil
	p.curIdx = 0
}

func (p *Parser) Result() (Config, []error) {
	p.state = Finalized
	return p.result, p.accumulatedErrors
}

func (p *Parser) getCurrent() string {
	if p.curIdx >= 0 && p.curIdx < len(p.currentFlags) {
		return p.currentFlags[p.curIdx]
	}
	return ""
}

func (p *Parser) setCurrent(value string) {
	if p.curIdx >= 0 && p.curIdx < len(p.currentFlags) {
		p.currentFlags[p.curIdx] = value
	}
}

func (p *Parser) getNext() string {
	p.curIdx++
	if p.curIdx >= 0 && p.curIdx < len(p.currentFlags) {
		return p.currentFlags[p.curIdx]
	}
	return ""
}

func (p *Parser) getNextValue(message string) (string, error) {
	p.curIdx++
	if p.curIdx >= 0 && p.curIdx < len(p.currentFlags) {
		return p.currentFlags[p.curIdx], nil
	}
	return "", errors.New(message)
}

func (p *Parser) parseShortFlag() {
	flag := p.getCurrent()

	if len(flag) > 2 {
		for _, b := range flag[1:] {
			switch b {
			case 'i':
				p.result.Insecure = true
			case 'v':
				p.result.Version = true
			case 'V':
				p.result.Reporter.Verbose = true
			case 'h':
				p.result.Help = true
			case 'l':
				p.result.List = true
			case 'u':
				p.result.Unrestricted = true
			default:
				p.accumulateError(fmt.Errorf("Unknown boolean flag: %c", b))
			}
		}
		return
	}

	switch flag[1] {
	case 'S':
		p.parseShow()
	case 'i':
		p.result.Insecure = true
	case 'v':
		p.result.Version = true
	case 'V':
		p.result.Reporter.Verbose = true
	case 'h':
		p.result.Help = true
	case 'l':
		p.result.List = true
	case 'u':
		p.result.Unrestricted = true
	case 'w':
		p.parseWorkerCount()
	case 's':
		p.parseSecret()
	case 'e':
		p.parseEnv()
	case 'o':
		path, err := p.getNextValue("No path passed for output file. Usage: --out|-o path/to/file")
		if err != nil {
			p.accumulateError(err)
			return
		}
		p.result.Out = path
	case 'L':
		p.parseLogLevel()
	case 'f':
		p.parseOutFormat()
	case 'c':
		p.parseColorOption()
	case 't':
		tag, err := p.getNextValue("No tag passed for filtering by tag. Usage: --tag|-t my-tag")
		if err != nil {
			p.accumulateError(err)
			return
		}

		p.result.TagsInclude = append(p.result.TagsInclude, tag)
	case 'n':
		nameRaw, err := p.getNextValue("No regex passed for filtering by name. Usage: --name|-n '^Custom Name$'")
		if err != nil {
			p.accumulateError(err)
			return
		}

		nameRegex, err := regexp.Compile(nameRaw)
		if err != nil {
			p.accumulateError(fmt.Errorf("Failed to compile regex for filtering by name: %w", err))
			return
		}
		if nameRegex == nil {
			p.accumulateError(errors.New("Regex for filtering by name did not compile, but returned no errors"))
			return
		}

		p.result.NamesInclude = append(p.result.NamesInclude, *nameRegex)
	default:
		p.accumulateError(fmt.Errorf("Unknown short flag: %c", flag[1]))
	}
}

func (p *Parser) parseEnv() {
	keyVal, err := p.getNextValue("No value passed for env value. Usage: --env|-e key=value")
	if err != nil {
		p.accumulateError(err)
		return
	}

	keyValSlice := strings.SplitN(keyVal, "=", 2)
	if len(keyValSlice) != 2 {
		p.accumulateError(fmt.Errorf("Failed to parse env value %s, could not split by '='. Usage: --env|-e key=value", keyVal))
		return
	}

	if keyValSlice[0] == "" {
		p.accumulateError(errors.New("Empty key passed to env. Usage: --env|-e key=value"))
		return
	}

	p.result.Treewalker.Env[keyValSlice[0]] = keyValSlice[1]
}

func (p *Parser) parseSecret() {
	keyVal, err := p.getNextValue("No value passed for secret value. Usage: --secret|-s key=value")
	if err != nil {
		p.accumulateError(err)
		return
	}

	keyValSlice := strings.SplitN(keyVal, "=", 2)
	if len(keyValSlice) != 2 {
		p.accumulateError(errors.New("Failed to parse secret value, could not split by '='. Usage: --secret|-s key=value"))
		return
	}

	if keyValSlice[0] == "" {
		p.accumulateError(errors.New("Empty key passed to secret. Usage: --secret|-s key=value"))
		return
	}

	p.result.Treewalker.Secret[keyValSlice[0]] = keyValSlice[1]
}

func (p *Parser) parseOutFormat() {
	format, err := p.getNextValue("No format passed for output. Usage: --format|-f junit")
	if err != nil {
		p.accumulateError(err)
		return
	}
	if !outFormats[format] {

		sb := strings.Builder{}
		hack := false
		for known := range outFormats {
			if hack {
				sb.WriteString(", ")
			}
			sb.WriteString(known)
			hack = true
		}

		p.accumulateError(fmt.Errorf("Unknown output format: %s. Known options: %s", format, sb.String()))
	} else {
		p.result.Format = format
	}
}

func (p *Parser) parseWorkerCount() {
	valueStr, err := p.getNextValue("No count passed for workers. Usage: --workers|-w 5")
	if err != nil {
		p.accumulateError(err)
		return
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		p.accumulateError(fmt.Errorf("Failed to parse worker count: %v", err))
		return
	}
	if value <= 0 {
		p.accumulateError(fmt.Errorf("Invalid worker count: %d", value))
		return
	}
	p.result.Workers = value
}

func (p *Parser) parseColorOption() {
	value, err := p.getNextValue("No color option passed for output. Usage: --color|-c always")
	if err != nil {
		p.accumulateError(err)
		return
	}

	switch strings.ToLower(value) {
	case "never":
		p.result.Reporter.Color = Never
	case "always":
		p.result.Reporter.Color = Always
	case "auto":
		p.result.Reporter.Color = Auto
	default:
		p.accumulateError(fmt.Errorf("Unknown color option: %s", value))
	}
}

func (p *Parser) parseLogLevel() {
	value, err := p.getNextValue("No log level passed. Usage: --log-level|-L debug")
	if err != nil {
		p.accumulateError(err)
		return
	}

	switch strings.ToLower(value) {
	case "debug":
		p.result.LogLevel = slog.LevelDebug
	case "info":
		p.result.LogLevel = slog.LevelInfo
	case "warn":
		p.result.LogLevel = slog.LevelWarn
	case "error":
		p.result.LogLevel = slog.LevelError
	default:
		p.accumulateError(fmt.Errorf("Unknown log level: %s", value))
	}
}

func (p *Parser) parseShow() {
	value, err := p.getNextValue("No show option passed. Usage: --show|-S all")
	if err != nil {
		p.accumulateError(err)
		return
	}

	switch strings.ToLower(value) {
	case "all":
		p.result.Reporter.Show = All
	case "no-skip":
		p.result.Reporter.Show = NoSkip
	case "failures":
		p.result.Reporter.Show = Failures
	default:
		p.accumulateError(fmt.Errorf("Unknown show option: %s", value))
	}
}

func (p *Parser) parseLongFlag() {
	flag := p.getCurrent()

	if strings.HasPrefix(flag, "--") {
		if len(flag) == 2 {
			p.state = Positional
			return
		}

		short := longToShort[flag]
		if short == "" {
			p.parseLongOnlyFlag()
			return
		}
		p.setCurrent(short)
	}

	p.parseShortFlag()
}

func (p *Parser) parseLongOnlyFlag() {
	flag := p.getCurrent()
	switch flag {
	case "--safe":
		p.result.Safe = true
	case "--skip-tag":
		tag, err := p.getNextValue("No tag passed for filtering by tag. Usage: --skip-tag my-tag")
		if err != nil {
			p.accumulateError(err)
			return
		}

		p.result.TagsExclude = append(p.result.TagsExclude, tag)
	case "--skip-name":
		nameRaw, err := p.getNextValue("No regex passed for filtering by name. Usage: --skip-name '^Custom Name$'")
		if err != nil {
			p.accumulateError(err)
			return
		}

		nameRegex, err := regexp.Compile(nameRaw)
		if err != nil {
			p.accumulateError(fmt.Errorf("Failed to compile regex for filtering by name: %w", err))
			return
		}
		if nameRegex == nil {
			p.accumulateError(errors.New("Regex for filtering by name did not compile, but returned no errors"))
			return
		}

		p.result.NamesExclude = append(p.result.NamesExclude, *nameRegex)
	case "--dump-on-failure":
		p.result.DumpOnFailure = true
	case "--secrets-file":
		path, err := p.getNextValue("No path passed for secrets. Usage: --secrets-file path/to/file")
		if err != nil {
			p.accumulateError(err)
			return
		}
		p.result.Treewalker.SecretsFile = path
	case "--plugins":
		path, err := p.getNextValue("No path passed for lua plugins. Usage: --plugins path/to/dir")
		if err != nil {
			p.accumulateError(err)
			return
		}
		p.result.LuaPaths = append(p.result.LuaPaths, strings.Split(path, ";")...)
	default:
		p.accumulateError(fmt.Errorf("Unknown option: %s", flag))
	}
}

func (p *Parser) parsePositional() {
	if p.state == Finalized {
		return
	}

	flag := p.getCurrent()
	if p.state == Flags {
		if strings.HasPrefix(flag, "-") {
			p.parseLongFlag()
			return
		}

		p.state = Positional
	}

	if p.state == Positional && flag != "" {
		p.result.Paths = append(p.result.Paths, flag)
	}
}

func (p *Parser) accumulateError(err error) {
	p.accumulatedErrors = append(p.accumulatedErrors, err)
}

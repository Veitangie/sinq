// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"errors"
	"fmt"
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
	"junit":   true,
	"default": true,
}

var longToShort map[string]string = map[string]string{
	"--workers":  "-w",
	"--safe":     "-s",
	"--insecure": "-i",
	"--secrets":  "-S",
	"--out":      "-o",
	"--format":   "-f",
	"--verbose":  "-V",
	"--color":    "-c",
	"--help":     "-h",
	"--version":  "-v",
	"--list":     "-l",
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

func (p *Parser) parseShortFlag() {
	flag := p.getCurrent()

	if len(flag) > 2 {
		for _, b := range flag[1:] {
			switch b {
			case 's':
				p.result.Safe = true
			case 'i':
				p.result.Insecure = true
			case 'v':
				p.result.Version = true
			case 'V':
				p.result.Verbose = true
			case 'h':
				p.result.Help = true
			case 'l':
				p.result.List = true
			default:
				p.accumulateError(fmt.Errorf("Unknown boolean flag: %c", b))
			}
		}
		return
	}

	switch flag[1] {
	case 's':
		p.result.Safe = true
	case 'i':
		p.result.Insecure = true
	case 'v':
		p.result.Version = true
	case 'V':
		p.result.Verbose = true
	case 'h':
		p.result.Help = true
	case 'l':
		p.result.List = true
	case 'w':
		value, err := strconv.Atoi(p.getNext())
		if err != nil {
			p.accumulateError(fmt.Errorf("Failed to parse workers: %v", err))
			return
		}
		p.result.Workers = value
	case 'S':
		path := p.getNext()
		if path == "" {
			p.accumulateError(errors.New("No path passed for secrets. Usage: --secets|-S path/to/file"))
			return
		}
		p.result.Secrets = path
	case 'o':
		path := p.getNext()
		if path == "" {
			p.accumulateError(errors.New("No path passed for output file. Usage: --out|-o path/to/file"))
			return
		}
		p.result.Out = path
	case 'f':
		format := p.getNext()
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
		}
	case 'c':
		p.parseColorOption()
	default:
		p.accumulateError(fmt.Errorf("Unknown short flag: %c", flag[1]))
	}
}

func (p *Parser) parseColorOption() {
	value := p.getNext()
	switch value {
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

func (p *Parser) parseLongFlag() {
	flag := p.getCurrent()

	if strings.HasPrefix(flag, "--") {
		if len(flag) == 2 {
			p.state = Positional
			return
		}

		short := longToShort[flag]
		if short == "" {
			p.accumulateError(fmt.Errorf("Unknown option: %s", flag))
			return
		}
		p.setCurrent(short)
	}

	p.parseShortFlag()
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

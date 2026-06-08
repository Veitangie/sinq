// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type parser struct {
	source            []byte
	current           int
	lineNumber        int
	offsetNumber      int
	currentScriptName string
	unnamedScriptIdx  int
}

func (p *parser) advance() {
	if p.current >= len(p.source) {
		return
	}
	current := p.source[p.current]
	p.offsetNumber++

	if current == '\n' {
		p.lineNumber++
		p.offsetNumber = 1
	}

	p.current++
}

func (p *parser) read() (byte, error) {
	if p.current >= len(p.source) {
		return 0b0, io.EOF
	}
	return p.source[p.current], nil
}

func (p *parser) match(expected byte) error {
	b, err := p.read()
	p.advance()
	if err != nil {
		return p.unexpectedEOF()
	}

	if b != expected {
		return p.scriptError(fmt.Sprintf("Unexpected character %c, expecting %c", b, expected))
	}

	return nil
}

func (p *parser) scriptError(message string) error {
	maybeScriptName := ""
	if p.currentScriptName != "" {
		maybeScriptName = fmt.Sprintf(" \"%s\"", p.currentScriptName)
	}
	return fmt.Errorf("%d:%d Failed to parse lua script%s: %s", p.lineNumber, p.offsetNumber, maybeScriptName, message)
}

func (p *parser) unexpectedEOF() error {
	return p.scriptError("Unexpected EOF")
}

func (bp *RequestBlueprint) setScript(t Token) error {
	switch t.Name {
	case "PRE":
		if bp.Pre.Type != IncompleteToken {
			return errors.New("Pre script is defined more than once")
		}
		bp.Pre = t
	case "RETRY":
		if bp.Retry.Type != IncompleteToken {
			return errors.New("Retry script is defined more than once")
		}
		bp.Retry = t
	case "ASSERT":
		if bp.Assert.Type != IncompleteToken {
			return errors.New("Assert script is defined more than once")
		}
		bp.Assert = t
	case "POST":
		if bp.Post.Type != IncompleteToken {
			return errors.New("Post script is defined more than once")
		}
		bp.Post = t
	default:
		bp.Content = append(bp.Content, t)
	}
	return nil
}

func (p *parser) lexToken() (Token, error) {
	b, err := p.read()
	if err != nil {
		return Token{Type: EOF, Line: p.lineNumber, Offset: p.offsetNumber, PayloadStart: -1, PayloadEnd: -1}, nil
	}

	switch b {
	case '$':
		return p.parseScript()
	default:
		return p.parseText()
	}
}

func (p *parser) parseText() (Token, error) {
	isEscaped := false
	hasEscapes := false
	res := Token{
		Type:         Text,
		Start:        p.current,
		PayloadStart: p.current,
		Line:         p.lineNumber,
		Offset:       p.offsetNumber,
	}
	for {
		b, err := p.read()
		if err != nil {
			res.End = p.current
			res.PayloadEnd = p.current
			res.HasEscapes = hasEscapes
			if isEscaped {
				err = fmt.Errorf("%d:%d Unexpected EOF after escape character", p.lineNumber, p.offsetNumber)
			} else {
				err = nil
			}
			return res, err
		}
		if isEscaped {
			isEscaped = false
			p.advance()
			continue
		}
		switch b {
		case '\\':
			isEscaped = true
			hasEscapes = true
		case '$':
			res.End = p.current
			res.PayloadEnd = p.current
			res.HasEscapes = hasEscapes
			return res, nil
		}
		p.advance()
	}
}

func (p *parser) parseScript() (Token, error) {
	res := Token{Start: p.current, PayloadStart: -1, PayloadEnd: -1, End: p.current + 1, Line: p.lineNumber, Offset: p.offsetNumber}
	err := p.match('$')
	if err != nil {
		res.End = p.current
		return res, err
	}

	name, err := p.parseScriptName()

	if len(name) == 0 {
		name = fmt.Sprintf("Unnamed_%d", p.unnamedScriptIdx)
		p.unnamedScriptIdx++
	}

	res.Name = name
	p.currentScriptName = name
	defer func() { p.currentScriptName = "" }()
	if err != nil {
		res.End = p.current
		return res, err
	}

	err = p.match('{')
	if err != nil {
		res.End = p.current
		return res, err
	}
	res.PayloadStart = p.current

	err = p.parseLuaScript()
	if err != nil {
		res.End = p.current
		return res, err
	}

	err = p.match('}')
	if err != nil {
		res.End = p.current
		return res, err
	}

	res.End = p.current
	res.PayloadEnd = p.current - 1
	res.Type = Script

	return res, nil
}

func (p *parser) parseScriptName() (string, error) {
	start := p.current
	startLine := p.lineNumber
	startOffset := p.offsetNumber
	for {
		b, err := p.read()
		if err != nil {
			return string(p.source[start:p.current]), p.unexpectedEOF()
		}

		if b == '\n' {
			return string(p.source[start:p.current]), fmt.Errorf("%d:%d: Expected start of lua script with {, got newline instead", startLine, startOffset)
		}

		// Script names can consist of any characters apart from {
		if b != '{' {
			p.advance()
			continue
		}

		break
	}

	return strings.TrimSpace(string(p.source[start:p.current])), nil
}

func (p *parser) parseLuaScript() error {
	for {
		b, err := p.read()
		if err != nil {
			return p.unexpectedEOF()
		}

		switch b {
		case '-':
			err := p.parseLuaComment()
			if err != nil {
				return err
			}

		case '"', '\'':
			err := p.parseLuaSimpleString()
			if err != nil {
				return err
			}

		case '[':
			level, err := p.parseLongBracketOpen()
			if err != nil {
				return err
			}

			if level < 0 {
				continue
			}

			err = p.parseLuaMultilineStringWithCloser(level)
			if err != nil {
				return err
			}

		case '{':
			p.advance()
			err := p.parseLuaScript()
			if err != nil {
				return err
			}

			err = p.match('}')
			if err != nil {
				return err
			}

		case '}':
			return nil

		default:
			p.advance()
		}
	}
}

func (p *parser) parseLuaComment() error {
	for range 2 {
		b, err := p.read()
		if err != nil {
			return p.unexpectedEOF()
		}

		if b != '-' {
			return nil
		}
		p.advance()
	}

	canBeBracket, err := p.read()
	if err != nil {
		return p.unexpectedEOF()
	}

	if canBeBracket == '[' {
		level, err := p.parseLongBracketOpen()
		if err != nil {
			return err
		}
		if level < 0 {
			return p.parseLuaSimpleComment()
		}

		err = p.parseLuaMultilineStringWithCloser(level)
		return err
	}

	return p.parseLuaSimpleComment()
}

func (p *parser) parseLuaSimpleComment() error {
	for {
		b, err := p.read()
		p.advance()

		if err != nil {
			return p.unexpectedEOF()
		}

		if b == '\n' {
			return nil
		}
	}
}

func (p *parser) parseLuaSimpleString() error {
	quote, err := p.read()
	if err != nil {
		return p.unexpectedEOF()
	}
	if quote != '"' && quote != '\'' {
		return p.scriptError("Unexpected start of string, expecting \" or ' at the start")
	}
	p.advance()

	startLine := p.lineNumber
	startOffset := p.offsetNumber
	isEscaped := false

	for {
		b, err := p.read()
		if err != nil {
			return p.scriptError(fmt.Sprintf("Unclosed string literal at pos %d:%d, expecting %c", startLine, startOffset, quote))
		}

		if b == '\n' {
			return p.scriptError(fmt.Sprintf("Unclosed string literal at pos %d:%d, expecting %c", startLine, startOffset, quote))
		}
		p.advance()

		if isEscaped {
			isEscaped = false
			continue
		}

		switch b {
		case quote:
			return nil
		case '\\':
			isEscaped = true
		}
	}
}

func (p *parser) parseLuaMultilineStringWithCloser(level int) error {
	if level < 0 {
		return p.scriptError(fmt.Sprintf("Multiline string can't be of negative level, got: %d", level))
	}

	closerLevel := -1
	for closerLevel != level {
		err := p.parseLuaMultilineString()
		if err != nil {
			return err
		}

		err = p.match(']')
		if err != nil {
			return err
		}

		nextCloserLevel, err := p.parseLongBracketClose()
		if err != nil {
			return err
		}
		closerLevel = nextCloserLevel
	}
	return nil
}

func (p *parser) parseLuaMultilineString() error {
	for {
		b, err := p.read()
		if err != nil {
			return p.unexpectedEOF()
		}

		if b == ']' {
			return nil
		}
		p.advance()
	}
}

func (p *parser) parseLongBracketOpen() (int, error) {
	err := p.match('[')
	if err != nil {
		return -1, err
	}

	layer := 0
	for {
		b, err := p.read()
		if err != nil {
			return -1, p.unexpectedEOF()
		}

		switch b {
		case '[':
			p.advance()
			return layer, nil
		case '=':
			layer++
		default:
			return -1, nil
		}
		p.advance()
	}
}

func (p *parser) parseLongBracketClose() (int, error) {
	layer := 0
	for {
		b, err := p.read()
		if err != nil {
			return -1, p.unexpectedEOF()
		}

		switch b {
		case ']':
			p.advance()
			return layer, nil
		case '=':
			layer++
		default:
			return -1, nil
		}
		p.advance()
	}
}

func (p *parser) consumeWhitespace() {
	for {
		b, err := p.read()
		if err != nil {
			return
		}
		if !unicode.IsSpace(rune(b)) {
			return
		}
		p.advance()
	}
}

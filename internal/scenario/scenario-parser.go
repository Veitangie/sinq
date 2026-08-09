// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Veitangie/sinq/internal/scanner"
)

type parser struct {
	*scanner.ByteScanner
	currentScriptName string
	unnamedScriptIdx  int
	delimiterToken    Token
}

func (p *parser) match(expected byte) error {
	b, err := p.Read()
	p.Advance()
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
	return fmt.Errorf("%d:%d Failed to parse lua script%s: %s", p.LineNumber, p.OffsetNumber, maybeScriptName, message)
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
	if p.delimiterToken.Type == Delimiter {
		res := p.delimiterToken
		p.delimiterToken = Token{}
		return res, nil
	}

	b, err := p.Read()
	if err != nil {
		return Token{Type: EOF, Line: p.LineNumber, Offset: p.OffsetNumber, PayloadStart: -1, PayloadEnd: -1}, nil
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
		Start:        p.Current,
		PayloadStart: p.Current,
		Line:         p.LineNumber,
		Offset:       p.OffsetNumber,
	}
	for {
		b, err := p.Read()
		if err != nil {
			res.End = p.Current
			res.PayloadEnd = p.Current
			res.HasEscapes = hasEscapes
			if isEscaped {
				err = fmt.Errorf("%d:%d Unexpected EOF after escape character", p.LineNumber, p.OffsetNumber)
			} else {
				err = nil
			}
			return res, err
		}

		if isEscaped {
			isEscaped = false
			p.Advance()
			continue
		}

		switch b {
		case '\\':
			isEscaped = true
			hasEscapes = true
		case '$':
			res.End = p.Current
			res.PayloadEnd = p.Current
			res.HasEscapes = hasEscapes
			return res, nil
		case '#':
			maybeEnd := p.Current
			maybeToken := p.parseDelimiter()

			if maybeToken.Type == Delimiter {
				if res.Start >= maybeEnd-1 {
					return maybeToken, nil
				}

				res.End = maybeEnd - 1
				res.PayloadEnd = maybeEnd - 1
				res.HasEscapes = hasEscapes

				p.delimiterToken = maybeToken
				return res, nil
			}

			isEscaped = p.Previous() == '\\'
			res.HasEscapes = maybeToken.HasEscapes || isEscaped
			continue
		}

		p.Advance()
	}
}

func (p *parser) parseDelimiter() Token {
	res := Token{
		Start: p.Current - 1,
		// This is a hack technically, since the actual delimiter starts at the new line, but for better UX we want
		// to report it as starting at the first #, so Line and Offset should match that in the result
		Line:   p.LineNumber,
		Offset: p.OffsetNumber,
	}

	prev := p.Previous()

	for range 3 {
		b, err := p.Read()
		if err != nil {
			return res
		}

		if b != '#' {
			return res
		}

		p.Advance()
	}

	if prev != '\n' && prev != 0b0 {
		return res
	}

	for {
		b, err := p.Read()

		if err != nil {
			return res
		}

		if b == '\n' {
			res.End = p.Current
			res.Type = Delimiter
			return res
		}

		if !unicode.IsSpace(rune(b)) {
			if res.PayloadStart == 0 {
				res.PayloadStart = p.Current
			}

			if b == '\\' {
				res.HasEscapes = true
			}
			res.PayloadEnd = p.Current + 1
		}

		p.Advance()
	}
}

func (p *parser) parseScript() (Token, error) {
	res := Token{Start: p.Current, PayloadStart: -1, PayloadEnd: -1, End: p.Current + 1, Line: p.LineNumber, Offset: p.OffsetNumber}
	err := p.match('$')
	if err != nil {
		res.End = p.Current
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
		res.End = p.Current
		return res, err
	}

	err = p.match('{')
	if err != nil {
		res.End = p.Current
		return res, err
	}
	res.PayloadStart = p.Current

	err = p.parseLuaScript()
	if err != nil {
		res.End = p.Current
		return res, err
	}

	err = p.match('}')
	if err != nil {
		res.End = p.Current
		return res, err
	}

	res.End = p.Current
	res.PayloadEnd = p.Current - 1
	res.Type = Script

	return res, nil
}

func (p *parser) parseScriptName() (string, error) {
	start := p.Current
	startLine := p.LineNumber
	startOffset := p.OffsetNumber
	for {
		b, err := p.Read()
		if err != nil {
			return string(p.Slice(start, p.Current)), p.unexpectedEOF()
		}

		if b == '\n' {
			return string(p.Slice(start, p.Current)), fmt.Errorf("%d:%d: Expected start of lua script with {, got newline instead", startLine, startOffset)
		}

		// Script names can consist of any characters apart from {
		if b != '{' {
			p.Advance()
			continue
		}

		break
	}

	return strings.TrimSpace(string(p.Slice(start, p.Current))), nil
}

func (p *parser) parseLuaScript() error {
	for {
		b, err := p.Read()
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
			p.Advance()
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
			p.Advance()
		}
	}
}

func (p *parser) parseLuaComment() error {
	for range 2 {
		b, err := p.Read()
		if err != nil {
			return p.unexpectedEOF()
		}

		if b != '-' {
			return nil
		}
		p.Advance()
	}

	canBeBracket, err := p.Read()
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
		b, err := p.Read()
		p.Advance()

		if err != nil {
			return p.unexpectedEOF()
		}

		if b == '\n' {
			return nil
		}
	}
}

func (p *parser) parseLuaSimpleString() error {
	quote, err := p.Read()
	if err != nil {
		return p.unexpectedEOF()
	}
	if quote != '"' && quote != '\'' {
		return p.scriptError("Unexpected start of string, expecting \" or ' at the start")
	}
	p.Advance()

	startLine := p.LineNumber
	startOffset := p.OffsetNumber
	isEscaped := false

	for {
		b, err := p.Read()
		if err != nil {
			return p.scriptError(fmt.Sprintf("Unclosed string literal at pos %d:%d, expecting %c", startLine, startOffset, quote))
		}

		if b == '\n' && !isEscaped {
			return p.scriptError(fmt.Sprintf("Unclosed string literal at pos %d:%d, expecting %c", startLine, startOffset, quote))
		}
		p.Advance()

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
		b, err := p.Read()
		if err != nil {
			return p.unexpectedEOF()
		}

		if b == ']' {
			return nil
		}
		p.Advance()
	}
}

func (p *parser) parseLongBracketOpen() (int, error) {
	err := p.match('[')
	if err != nil {
		return -1, err
	}

	layer := 0
	for {
		b, err := p.Read()
		if err != nil {
			return -1, p.unexpectedEOF()
		}

		switch b {
		case '[':
			p.Advance()
			return layer, nil
		case '=':
			layer++
		default:
			return -1, nil
		}
		p.Advance()
	}
}

func (p *parser) parseLongBracketClose() (int, error) {
	layer := 0
	for {
		b, err := p.Read()
		if err != nil {
			return -1, p.unexpectedEOF()
		}

		switch b {
		case ']':
			p.Advance()
			return layer, nil
		case '=':
			layer++
		default:
			return -1, nil
		}
		p.Advance()
	}
}

func (p *parser) consumeWhitespace() {
	for {
		b, err := p.Read()
		if err != nil {
			return
		}
		if !unicode.IsSpace(rune(b)) {
			return
		}
		p.Advance()
	}
}

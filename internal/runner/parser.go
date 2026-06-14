// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"unicode"
)

type requestParser struct {
	source     []byte
	current    int
	lineNumber int
	ctx        context.Context
}

const userAgent string = "The Spanish Inquisition/1.0"

func coalesceErrors(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func newParser(source []byte, ctx context.Context) (*requestParser, error) {
	if ctx == nil {
		return nil, errors.New("Failed to construct parser: context is nil")
	}
	return &requestParser{
		source:     source,
		current:    0,
		lineNumber: 1,
		ctx:        ctx,
	}, nil
}

func (r *requestParser) getCurrent() (byte, error) {
	if r.current >= len(r.source) {
		return 0b00, io.EOF
	}
	return r.source[r.current], nil
}

func (r *requestParser) advance() {
	if r.current >= len(r.source) {
		return
	}

	r.current++
	if b, _ := r.getCurrent(); b == '\n' {
		r.lineNumber++
	}
}

func (r *requestParser) scanWord() ([]byte, error) {
	r.skipWhitespace()
	start := r.current
	for {
		b, err := r.getCurrent()
		if err != nil || unicode.IsSpace(rune(b)) {
			return r.source[start:r.current], err
		}
		r.advance()
	}
}

func (r *requestParser) scanLine() ([]byte, error) {
	r.skipWhitespace()
	start := r.current
	lastNonSpace := r.current
	for {
		b, err := r.getCurrent()
		r.advance()
		if err != nil {
			return r.source[start:r.current], err
		}
		if b == ' ' || b == '	' || b == '\r' {
			continue
		}
		if b == '\n' {
			return r.source[start:lastNonSpace], nil
		}
		lastNonSpace = r.current
	}
}

func (r *requestParser) nextLine() error {
	r.skipWhitespace()
	linebreak, err := r.getCurrent()
	if err != nil {
		return err
	}
	if linebreak == '\r' {
		r.advance()
		linebreak, err = r.getCurrent()
		if err != nil {
			return err
		}
	}
	if linebreak != '\n' {
		return r.parsingError("Expected next line")
	}
	r.advance()
	return nil
}

func (r *requestParser) parsingError(message string) error {
	return fmt.Errorf("%d: %s", r.lineNumber, message)
}

func (r *requestParser) unexpectedEOF() error {
	return r.parsingError("Unexpected end of file")
}

func (r *requestParser) parse() (http.Request, []byte, error) {
	res := http.Request{}
	res = *res.WithContext(r.ctx)
	var body []byte
	err := coalesceErrors(r.scanRequestLine(&res), r.ctx.Err())
	if err != nil {
		return res, body, err
	}

	err = coalesceErrors(r.nextLine(), r.ctx.Err())
	if err != nil {
		if err == io.EOF {
			return res, body, nil
		}
		return res, body, err
	}

	err = coalesceErrors(r.scanHeaders(&res), r.ctx.Err())
	if err != nil {
		return res, body, err
	}

	err = coalesceErrors(r.nextLine(), r.ctx.Err())
	if err != nil || r.ctx.Err() != nil {
		if err == io.EOF {
			return res, body, nil
		}
		return res, body, err
	}

	contentLength := len(r.source) - r.current
	if contentLength > 0 {
		body = make([]byte, contentLength)
		copy(body, r.source[r.current:])

		res.ContentLength = int64(contentLength)
	}

	return res, body, nil
}

func (r *requestParser) scanRequestLine(dest *http.Request) error {
	contents := make([]string, 0, 3)
	for range 3 {
		word, err := r.scanWord()
		err = coalesceErrors(err, r.ctx.Err())
		if err != nil {
			return r.unexpectedEOF()
		}
		if len(word) == 0 {
			break
		}
		contents = append(contents, string(word))
	}

	if len(contents) == 0 {
		return r.parsingError("Empty request line")
	}

	contentsIdx := 0
	dest.Method = http.MethodGet
	if len(contents) > 1 {
		dest.Method = contents[contentsIdx]
		contentsIdx++
	}

	actualURL, err := url.Parse(contents[contentsIdx])
	if err != nil {
		return r.parsingError("Failed to parse url")
	}

	err = r.validateURL(actualURL)
	if err != nil {
		return err
	}

	dest.Host = actualURL.Host
	dest.URL = actualURL

	return nil
}

func (r *requestParser) scanHeaders(dest *http.Request) error {
	dest.Header = http.Header{http.CanonicalHeaderKey("User-Agent"): []string{userAgent}}
	for {
		name, err := r.scanWord()
		err = coalesceErrors(err, r.ctx.Err())
		if len(name) == 0 {
			return nil
		}

		if err == io.EOF {
			return r.unexpectedEOF()
		} else if err != nil {
			return err
		}

		if name[len(name)-1] != ':' {
			return r.parsingError("Malformed header field, expecting ':' right after header field name")
		}
		name = name[:len(name)-1]

		value, _ := r.scanLine()

		if string(bytes.ToLower(name)) == "host" {
			dest.Host = string(value)
			continue
		}

		if string(bytes.ToLower(name)) == "user-agent" {
			dest.Header.Set(string(name), string(value))
			continue
		}

		dest.Header.Add(string(name), string(value))
	}
}

func (r *requestParser) validateURL(full *url.URL) error {
	if !full.IsAbs() {
		return r.parsingError("URL is not absolute, unable to derive scheme")
	}
	if len(full.Host) == 0 {
		return r.parsingError("Empty host in URL")
	}
	return nil
}

func (r *requestParser) skipWhitespace() {
	for {
		b, err := r.getCurrent()
		if err != nil || (b != ' ' && b != '	') {
			return
		}
		r.advance()
	}
}

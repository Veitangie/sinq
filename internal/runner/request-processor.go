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
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type RequestProcessor struct {
	w                 *worker
	ctx               context.Context
	scenarioBp        *scenario.ScenarioBlueprint
	requestBp         *scenario.RequestBlueprint
	requestIdx        int
	status            *ResultStatus
	result            *RequestResult
	requestTimer      timer.Timer
	totalRequestTimer timer.Timer
	materialized      []byte
	httpRequest       *http.Request
	requestBody       []byte
	multipart         multipartData
	retryIn           int64
	retries           int
	client            *http.Client
	filenameFrom      string
	filenameTo        string
	hash              string
	cache             bool
	skip              bool
}

func (p *RequestProcessor) handleError(err error) error {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			*p.status = Aborted
			p.result.Status = Aborted
		} else {
			*p.status = Error
			p.result.Status = Error
			p.result.ErrorMessage = err.Error()
		}
	}
	return err
}

func (p *RequestProcessor) runPre() error {
	if p.requestBp.Pre.IsEmpty() {
		return nil
	}

	p.w.env.logger.Debug("[Runner] Worker running pre script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	res, err := p.w.runPreScript(p.requestBp.Pre, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Pre = p.requestTimer.Time()
	p.totalRequestTimer = p.requestTimer

	if err != nil {
		p.w.env.logger.Debug("[Runner] Pre script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	p.filenameFrom = res.filenameIn
	p.filenameTo = res.filenameOut
	p.cache = res.cache
	p.skip = res.skip
	p.multipart = res.multipart
	return p.handleError(err)
}

func (p *RequestProcessor) materialize() error {
	p.w.env.logger.Debug("[Runner] Worker materializing request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	materialized, err := p.w.materializeRequest(p.ctx, p.requestBp, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.materialized = materialized
	p.result.Materialization = p.requestTimer.Time()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Request materialization failed", p.w.loggingContextWithErr(p.ctx, err)...)
	} else if p.w.env.logger.Enabled(p.ctx, slog.LevelDebug) {
		p.w.env.logger.Debug("[Runner] Request materialized successfully", append(p.w.loggingContext(p.ctx), "raw", string(materialized))...)
	}

	if p.w.env.cfg.DumpOnFailure {
		if len(materialized) != 0 && materialized[len(materialized)-1] != '\n' {
			p.result.Request = string(append(materialized, '\n'))
		} else {
			p.result.Request = string(materialized)
		}
	}

	return p.handleError(err)
}

func (p *RequestProcessor) parse() error {
	p.w.env.logger.Debug("[Runner] Worker parsing request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	parser, err := newParser(p.materialized, p.ctx)
	if err != nil {
		p.w.env.logger.Warn("Failed to construct parser", p.w.loggingContextWithErr(p.ctx, err)...)
		return p.handleError(err)
	}

	httpReq, body, err := parser.parse()
	p.result.Parsing = p.requestTimer.Time()

	if err == nil && len(body) > 0 && p.filenameFrom != "" {
		err = fmt.Errorf("Request has both body and a file source")
	}

	if err != nil {
		p.w.env.logger.Debug("[Runner] Request parsing failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	p.httpRequest = &httpReq
	p.requestBody = body

	return p.handleError(err)
}

func (p *RequestProcessor) run() error {
	p.w.env.logger.Debug("[Runner] Sending request", p.w.loggingContext(p.ctx)...)

	var result intermediate
	var err error
	for p.retryIn >= 0 {
		if p.retries > p.scenarioBp.Config.MaxRetries {
			err = errors.New("Too many retries")
			p.w.env.logger.Debug("[Runner] Too many retries", p.w.loggingContext(p.ctx)...)
			return p.handleError(err)
		}
		p.retries++

		if p.cache {
			result, err = p.sendCached()
			if err != nil {
				return p.handleError(err)
			}
		} else {
			result, err = p.send()
			if err != nil {
				return p.handleError(err)
			}
		}

		result.attempt = p.retries

		if p.w.env.logger.Enabled(p.ctx, slog.LevelDebug) {
			p.w.env.logger.Debug("[Runner] Extracted response", append(p.w.loggingContext(p.ctx), "code", result.statusCode, "headers", result.headers, "body", string(result.body))...)
		}

		p.w.env.logger.Debug("[Runner] Worker capturing response", p.w.loggingContext(p.ctx)...)
		if len(result.body) > int(p.scenarioBp.Config.MaxBodySize.ByteAmount) {
			result.body = result.body[:p.scenarioBp.Config.MaxBodySize.ByteAmount]
			result.oversized = true
		}

		responseStr, err := p.w.requestCompleted(result)
		p.result.Response = responseStr
		if err != nil {
			p.w.env.logger.Debug("[Runner] Failed to capture response", p.w.loggingContextWithErr(p.ctx, err)...)
			return err
		}

		if err = p.runRetry(); err != nil {
			return p.handleError(err)
		}

		if p.retryIn > 0 {
			timer := time.NewTimer(time.Duration(p.retryIn) * time.Millisecond)
			p.w.env.logger.Debug("[Runner] Waiting for retry", append(p.w.loggingContext(p.ctx), "retryIn", p.retryIn)...)
			select {
			case <-timer.C:
				timer.Stop()
				continue
			case <-p.ctx.Done():
				timer.Stop()
				return p.handleError(p.ctx.Err())
			}
		}
	}
	return nil
}

func (p *RequestProcessor) getHash() string {
	if p.hash == "" {
		p.w.env.hasher.Write(p.materialized)
		p.w.env.hasher.WriteString(p.filenameFrom)
		p.w.env.hasher.WriteString(p.filenameTo)
		p.w.env.hasher.WriteString(p.w.env.workspace.String())
		if p.client.Jar != nil {
			for _, cookie := range p.client.Jar.Cookies(p.httpRequest.URL) {
				p.w.env.hasher.WriteString(cookie.Name)
				p.w.env.hasher.WriteString(cookie.Value)
			}
		}

		for _, entry := range p.multipart.entries {
			p.w.env.hasher.WriteString(entry.data)
			p.w.env.hasher.WriteString(entry.source)
			p.w.env.hasher.WriteString(entry.contentDisposition)
			p.w.env.hasher.WriteString(entry.contentType)
			keys := make([]string, 0, len(entry.headers))
			for key := range entry.headers {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				p.w.env.hasher.WriteString(key)
				for _, value := range entry.headers[key] {
					p.w.env.hasher.WriteString(value)
				}
			}
		}
		p.hash = strconv.FormatUint(p.w.env.hasher.Sum64(), 16)
		p.w.env.hasher.Reset()
	}

	return p.hash
}

func (p *RequestProcessor) validateBody() error {
	hasFile := p.filenameFrom != ""
	hasLiteral := len(p.requestBody) > 0
	hasMultipart := len(p.multipart.entries) != 0
	noBody := !hasFile && !hasLiteral && !hasMultipart
	if !noBody && !xorAll(hasFile, hasLiteral, hasMultipart) {
		bodyTypes := make([]string, 0, 3)
		if hasFile {
			bodyTypes = append(bodyTypes, "file")
		}
		if hasLiteral {
			bodyTypes = append(bodyTypes, "literal")
		}
		if hasMultipart {
			bodyTypes = append(bodyTypes, "multipart")
		}

		return fmt.Errorf("Request has more than one body type (found body types: %s)", strings.Join(bodyTypes, ", "))
	}
	return nil
}

func (p *RequestProcessor) materializeMultipart() error {
	if len(p.multipart.materialized) != 0 {
		return nil
	}
	if len(p.multipart.entries) != 0 {
		body, contentType, err := makeMultipartBody(p.multipart, p.w.env.workspace)
		if err != nil {
			return err
		}
		p.multipart.materialized = body
		if contentTypes := p.httpRequest.Header.Values(contentTypeHeader); len(contentTypes) == 0 {
			p.httpRequest.Header.Set(contentTypeHeader, contentType)
		}
		p.httpRequest.ContentLength = int64(len(body))
	}
	return nil
}

func (p *RequestProcessor) send() (intermediate, error) {
	var result intermediate
	p.w.env.logger.Debug("[Runner] Worker sending request", append(p.w.loggingContext(p.ctx), "attempt", p.retries)...)

	if err := p.validateBody(); err != nil {
		return result, p.handleError(err)
	}

	if err := p.materializeMultipart(); err != nil {
		return result, p.handleError(err)
	}

	if p.filenameFrom != "" {
		file, err := p.w.env.workspace.Open(p.filenameFrom)

		if err != nil {
			p.w.env.logger.Debug("[Runner] Failed to open file for reading", append(p.w.loggingContextWithErr(p.ctx, err), "filename", p.filenameFrom)...)
			return result, p.handleError(err)
		}

		p.httpRequest.ContentLength = -1
		p.httpRequest.Body = file
		p.httpRequest.GetBody = func() (io.ReadCloser, error) { return p.w.env.workspace.Open(p.filenameFrom) }

	} else if len(p.multipart.materialized) != 0 {
		p.httpRequest.Body = io.NopCloser(bytes.NewReader(p.multipart.materialized))
		p.httpRequest.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.multipart.materialized)), nil }
	} else {
		p.httpRequest.Body = io.NopCloser(bytes.NewReader(p.requestBody))
		p.httpRequest.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.requestBody)), nil }
	}

	p.requestTimer.Start()
	resp, err := p.client.Do(p.httpRequest)
	p.result.Execution += p.requestTimer.Time()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Request execution failed", p.w.loggingContextWithErr(p.ctx, err)...)
		return result, p.handleError(fixClientErr(err))
	}

	return makeIntermediate(p.w.env.workspace, p.filenameTo, int64(p.scenarioBp.Config.MaxBodySize.ByteAmount), resp)
}

func (p *RequestProcessor) sendCached() (intermediate, error) {
	var result intermediate
	var getBody func(Workspace) func() (io.ReadCloser, error)
	if err := p.validateBody(); err != nil {
		return result, err
	}

	if err := p.materializeMultipart(); err != nil {
		return result, err
	}

	if p.filenameFrom != "" {
		p.httpRequest.ContentLength = -1
		getBody = func(w Workspace) func() (io.ReadCloser, error) {
			return func() (io.ReadCloser, error) { return w.Open(p.filenameFrom) }
		}
	} else if len(p.multipart.materialized) != 0 {
		getBody = func(w Workspace) func() (io.ReadCloser, error) {
			return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.multipart.materialized)), nil }
		}
	} else {
		getBody = func(w Workspace) func() (io.ReadCloser, error) {
			return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.requestBody)), nil }
		}
	}

	actualKey := p.getHash() + strconv.Itoa(p.retries)
	if fromCache, err, ok := p.w.env.cachedProcessor.lookup(actualKey); ok {
		if err != nil {
			return result, err
		}
		result = fromCache
	} else {
		if p.client.Jar != nil {
			for _, cookie := range p.client.Jar.Cookies(p.httpRequest.URL) {
				p.httpRequest.AddCookie(cookie)
			}
		}

		p.requestTimer.Start()
		select {
		case res := <-p.w.env.cachedProcessor.process(actualKey, *p.httpRequest, p.filenameTo, getBody, p.w.env.workspace):
			if res.Err != nil {
				return result, p.handleError(res.Err)
			}
			result = res.Val.(intermediate)

		case <-p.ctx.Done():
			p.result.Execution += p.requestTimer.Time()
			return result, p.handleError(p.ctx.Err())
		}
		p.result.Execution += p.requestTimer.Time()
	}

	if p.client.Jar != nil {
		setCookies := result.headers.Values("set-cookie")
		cookies := make([]*http.Cookie, 0, len(setCookies))

		for _, setCookie := range setCookies {
			cookie, err := http.ParseSetCookie(setCookie)
			if err != nil {
				p.w.env.logger.Debug("[Runner] Failed to set cookies", p.w.loggingContextWithErr(p.ctx, err)...)
			} else {
				cookies = append(cookies, cookie)
			}
		}

		p.client.Jar.SetCookies(p.httpRequest.URL, cookies)
	}

	return result, nil
}

func (p *RequestProcessor) runRetry() error {
	if p.requestBp.Retry.IsEmpty() {
		p.retryIn = -1
		return nil
	}
	p.w.env.logger.Debug("[Runner] Worker running retry script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	retryIn, err := p.w.runRetryScript(p.requestBp.Retry, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Retry += p.requestTimer.Time()
	p.retryIn = retryIn

	if err != nil {
		p.w.env.logger.Debug("[Runner] Retry script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) runAssert() error {
	if p.requestBp.Assert.IsEmpty() {
		return nil
	}
	p.w.env.logger.Debug("[Runner] Worker running assert script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	err := p.w.runAssertScript(p.requestBp.Assert, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration, p.filenameTo)
	p.result.Assert = p.requestTimer.Time()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Assert script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}
	return p.handleError(err)
}

func (p *RequestProcessor) runPost() error {
	if p.requestBp.Post.IsEmpty() {
		return nil
	}
	p.w.env.logger.Debug("[Runner] Worker running post script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	err := p.w.runEffectfulScript(p.requestBp.Post, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Post = p.requestTimer.Time()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Post script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}
	return p.handleError(err)
}

func (p *RequestProcessor) finalize() {
	if p.result.Status == Unset {
		p.result.Status = Success
	}
	p.result.Total = p.totalRequestTimer.Time()
}

func fixClientErr(err error) error {
	if urlError, ok := errors.AsType[*url.Error](err); ok {
		if u, perr := url.Parse(urlError.URL); perr == nil {
			u.RawQuery = ""
			u.User = nil
			urlError.URL = u.String()
		}
		urlError.URL = strings.Split(urlError.URL, "?")[0]
	}
	return err
}

func xorAll(bs ...bool) bool {
	found := false
	for _, b := range bs {
		if found && b {
			return false
		}
		if b {
			found = true
		}
	}
	return found
}

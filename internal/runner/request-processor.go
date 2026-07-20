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
	"strconv"
	"strings"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
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
		*p.status = Error
		p.result.Status = Error
		p.result.ErrorMessage = err.Error()
	}
	return err
}

func (p *RequestProcessor) runPre() error {
	p.w.env.logger.Debug("[Runner] Worker running pre script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	filenameFrom, filenameTo, cache, skip, err := p.w.runPreScript(p.requestBp.Pre, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Pre = p.requestTimer.Time()
	p.totalRequestTimer = p.requestTimer
	p.result.StartedAt = p.requestTimer.StartedAt()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Pre script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	p.filenameFrom = filenameFrom
	p.filenameTo = filenameTo
	p.cache = cache
	p.skip = skip
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
		p.result.Request = string(materialized)
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
				*p.status = Aborted
				p.result.Status = Aborted
				return errors.New("Context aborted while waiting for retry")
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
		p.hash = strconv.FormatUint(p.w.env.hasher.Sum64(), 16)
		p.w.env.hasher.Reset()
	}

	return p.hash
}

func (p *RequestProcessor) send() (intermediate, error) {
	var result intermediate
	p.w.env.logger.Debug("[Runner] Worker sending request", append(p.w.loggingContext(p.ctx), "attempt", p.retries)...)

	if p.filenameFrom != "" && p.httpRequest.ContentLength != 0 {
		return result, p.handleError(errors.New("Request has both attached body and body content in its .sinq file"))
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

	} else if len(p.requestBody) != 0 && p.httpRequest.ContentLength != 0 {

		p.httpRequest.Body = io.NopCloser(bytes.NewReader(p.requestBody))
		p.httpRequest.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.requestBody)), nil }

	} else {
		p.httpRequest.Body = nil
	}

	p.requestTimer.Start()
	resp, err := p.client.Do(p.httpRequest)
	p.result.Execution += p.requestTimer.Time()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Request execution failed", p.w.loggingContextWithErr(p.ctx, err)...)
		if urlError, ok := errors.AsType[*url.Error](err); ok {
			urlError.URL = strings.Split(urlError.URL, "?")[0]
		}
		return result, p.handleError(err)
	}

	return makeIntermediate(p.w.env.workspace, p.filenameTo, int64(p.scenarioBp.Config.MaxBodySize.ByteAmount), resp)
}

func (p *RequestProcessor) sendCached() (intermediate, error) {
	var result intermediate
	var getBody func(Workspace) func() (io.ReadCloser, error)
	if p.filenameFrom != "" {
		getBody = func(w Workspace) func() (io.ReadCloser, error) {
			return func() (io.ReadCloser, error) { return w.Open(p.filenameFrom) }
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

		p.requestTimer.Start()
		select {
		case res := <-p.w.env.cachedProcessor.process(actualKey, *p.httpRequest, p.filenameTo, getBody, p.w.env.workspace):
			if res.Err != nil {
				return result, p.handleError(res.Err)
			}
			result = res.Val.(intermediate)

		case <-p.ctx.Done():
			*p.status = Aborted
			p.result.Status = Aborted
			p.result.Execution = p.requestTimer.Time()
			return result, errors.New("Context aborted while waiting for request to complete")
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
	if p.result.Status == Skipped {
		p.result.Status = Success
	}
	p.result.Total = p.totalRequestTimer.Time()
}

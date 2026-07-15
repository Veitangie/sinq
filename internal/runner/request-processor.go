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

type intermediate struct {
	status     string
	statusCode int
	headers    http.Header
	proto      string
	filenameTo string
	size       int64
	oversized  bool
	body       []byte
	attempt    int
}

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
	singleFlight      bool
	sentRequest       bool
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
	filenameFrom, filenameTo, singleFlight, err := p.w.runPreScript(p.requestBp.Pre, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Pre = p.requestTimer.Time()
	p.totalRequestTimer = p.requestTimer
	p.result.StartedAt = p.requestTimer.StartedAt()

	if err != nil {
		p.w.env.logger.Debug("[Runner] Pre script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	p.filenameFrom = filenameFrom
	p.filenameTo = filenameTo
	p.singleFlight = singleFlight
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

func (p *RequestProcessor) doRequest() error {
	var result *intermediate
	var err error
	if p.singleFlight {

		p.w.env.hasher.Write(p.materialized)
		p.w.env.hasher.WriteString(p.filenameFrom)
		p.w.env.hasher.WriteString(p.filenameTo)
		key := p.w.env.hasher.Sum64()
		p.w.env.hasher.Reset()

		var anyResult any
		anyResult, err, _ = p.w.env.singleFlight.Do(strconv.FormatUint(key, 16), p.run)
		result, _ = anyResult.(*intermediate)
	} else {
		var anyResult any
		anyResult, err = p.run()
		result, _ = anyResult.(*intermediate)
	}

	if err != nil {
		return err
	}

	if !p.sentRequest {
		p.w.env.logger.Debug("[Runner] Capturing response", p.w.loggingContext(p.ctx)...)
		responseStr, err := p.w.requestCompleted(*result)
		p.result.Response = responseStr
		if err != nil {
			p.w.env.logger.Debug("[Runner] Failed to capture response", p.w.loggingContextWithErr(p.ctx, err)...)
			return err
		}
	}
	return nil
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

func (p *RequestProcessor) send() (*http.Response, error) {
	p.w.env.logger.Debug("[Runner] Worker sending request", append(p.w.loggingContext(p.ctx), "attempt", p.retries)...)

	if p.filenameFrom != "" && p.httpRequest.ContentLength != 0 {
		return nil, p.handleError(errors.New("Request has both attached body and body content in its .sinq file"))
	}

	if p.filenameFrom != "" {
		file, err := p.w.env.workspace.Open(p.filenameFrom)

		if err != nil {
			p.w.env.logger.Debug("[Runner] Failed to open file for reading", append(p.w.loggingContextWithErr(p.ctx, err), "filename", p.filenameFrom)...)
			return nil, p.handleError(err)
		}

		p.httpRequest.ContentLength = -1
		p.httpRequest.Body = file
	} else if len(p.requestBody) != 0 && p.httpRequest.ContentLength != 0 {
		p.httpRequest.Body = io.NopCloser(bytes.NewReader(p.requestBody))
	} else {
		p.httpRequest.Body = nil
	}

	p.requestTimer.Start()
	resp, err := p.client.Do(p.httpRequest)
	p.result.Execution = p.requestTimer.Time()
	p.sentRequest = true

	if err != nil {
		p.w.env.logger.Debug("[Runner] Request execution failed", p.w.loggingContextWithErr(p.ctx, err)...)
		if urlError, ok := errors.AsType[*url.Error](err); ok {
			urlError.URL = strings.Split(urlError.URL, "?")[0]
		}
		return nil, p.handleError(err)
	}

	return resp, p.handleError(err)
}

func (p *RequestProcessor) runRetry() error {
	p.w.env.logger.Debug("[Runner] Worker running retry script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	retryIn, err := p.w.runRetryScript(p.requestBp.Retry, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.result.Retry = p.requestTimer.Time()
	p.retryIn = retryIn

	if err != nil {
		p.w.env.logger.Debug("[Runner] Retry script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) makeIntermediate(resp *http.Response) (intermediate, error) {
	defer resp.Body.Close()
	result := intermediate{
		statusCode: resp.StatusCode,
		status:     resp.Status,
		proto:      resp.Proto,
		headers:    resp.Header,
	}

	if p.filenameTo != "" {
		written, err := p.w.captureBodyToFile(resp.Body, p.filenameTo)
		if err != nil {
			return result, err
		}
		result.filenameTo = p.filenameTo
		result.size = written
		return result, nil
	}

	limited := io.LimitReader(resp.Body, int64(p.scenarioBp.Config.MaxBodySize.ByteAmount)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return result, err
	}

	if uint64(len(data)) > p.scenarioBp.Config.MaxBodySize.ByteAmount {
		data = data[:len(data)-1]
		io.Copy(io.Discard, resp.Body)
		result.oversized = true
	}

	result.body = data
	return result, nil
}

func (p *RequestProcessor) run() (any, error) {
	err := p.parse()
	if err != nil {
		return nil, err
	}

	result := intermediate{
		filenameTo: p.filenameTo,
	}
	p.w.env.logger.Debug("[Runner] Sending request", p.w.loggingContext(p.ctx)...)
	for p.retryIn >= 0 {
		if p.retries > p.scenarioBp.Config.MaxRetries {
			err := errors.New("Too many retries")
			p.w.env.logger.Debug("[Runner] Too many retries", p.w.loggingContext(p.ctx)...)
			return nil, p.handleError(err)
		}
		p.retries++

		resp, err := p.send()
		if err != nil {
			return nil, p.handleError(err)
		}

		result, err = p.makeIntermediate(resp)
		if err != nil {
			return nil, p.handleError(err)
		}
		result.attempt = p.retries

		if p.w.env.logger.Enabled(p.ctx, slog.LevelDebug) {
			p.w.env.logger.Debug("[Runner] Extracted response", append(p.w.loggingContext(p.ctx), "code", result.statusCode, "headers", result.headers, "body", string(result.body))...)
		}

		p.w.env.logger.Debug("[Runner] Worker capturing response", p.w.loggingContext(p.ctx)...)
		responseStr, err := p.w.requestCompleted(result)
		p.result.Response = responseStr
		if err != nil {
			p.w.env.logger.Debug("[Runner] Failed to capture response", p.w.loggingContextWithErr(p.ctx, err)...)
			return nil, err
		}

		if err := p.runRetry(); err != nil {
			return nil, p.handleError(err)
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
				return nil, errors.New("Context aborted while waiting for retry")
			}
		}
	}
	return &result, nil
}

func (p *RequestProcessor) runAssert() error {
	p.w.env.logger.Debug("[Runner] Worker running assert script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.Start()
	err := p.w.runAssertScript(p.requestBp.Assert, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
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

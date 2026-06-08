// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
)

type RequestProcessor struct {
	w                 *worker
	ctx               context.Context
	scenarioBp        *scenario.ScenarioBlueprint
	requestBp         *scenario.RequestBlueprint
	requestIdx        int
	status            *ResultStatus
	result            *RequestResult
	requestTimer      ResultTimer
	totalRequestTimer ResultTimer
	materialized      []byte
	httpRequest       *http.Request
	requestBody       []byte
	retryIn           int64
	retries           int
	client            *http.Client
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
	p.w.logger.Debug("Worker running pre script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	err := p.w.runEffectfulScript(p.requestBp.Pre, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.Timeout.Duration)
	p.result.Pre = p.requestTimer.Time()
	p.totalRequestTimer = p.requestTimer
	p.result.StartedAt = p.requestTimer.at

	if err != nil {
		p.w.logger.Debug("Pre script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}
	return p.handleError(err)
}

func (p *RequestProcessor) materialize() error {
	p.w.logger.Debug("Worker materializing request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	materialized, err := p.w.materializeRequest(p.ctx, p.requestBp, p.scenarioBp.Config.Timeout.Duration)
	p.materialized = materialized
	p.result.Materialization = p.requestTimer.Time()

	if err != nil {
		p.w.logger.Debug("Request materialization failed", p.w.loggingContextWithErr(p.ctx, err)...)
	} else if p.w.logger.Enabled(p.ctx, slog.LevelDebug) {
		p.w.logger.Debug("Request materialized successfully", append(p.w.loggingContext(p.ctx), "raw", string(materialized))...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) parse() error {
	p.w.logger.Debug("Worker parsing request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	parser, err := newParser(p.materialized, p.ctx)
	if err != nil {
		p.w.logger.Warn("Failed to construct parser", p.w.loggingContextWithErr(p.ctx, err)...)
		return p.handleError(err)
	}

	httpReq, body, err := parser.parse()
	p.result.Parsing = p.requestTimer.Time()
	p.httpRequest = &httpReq
	p.requestBody = body

	if err != nil {
		p.w.logger.Debug("Request parsing failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) send() error {
	p.httpRequest.Body = io.NopCloser(bytes.NewReader(p.requestBody))
	p.w.logger.Debug("Worker sending request", append(p.w.loggingContext(p.ctx), "attempt", p.retries)...)
	p.requestTimer.start()
	resp, err := p.client.Do(p.httpRequest)
	p.result.Execution = p.requestTimer.Time()

	if err != nil {
		p.w.logger.Debug("Request execution failed", p.w.loggingContextWithErr(p.ctx, err)...)
		if urlError, ok := errors.AsType[*url.Error](err); ok {
			urlError.URL = strings.Split(urlError.URL, "?")[0]
		}
		return p.handleError(err)
	}

	p.w.logger.Debug("Worker capturing response", p.w.loggingContext(p.ctx)...)
	err = p.w.requestCompleted(p.ctx, resp, p.requestIdx)
	if err != nil {
		p.w.logger.Debug("Failed to capture response", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) runRetry() error {
	p.w.logger.Debug("Worker running retry script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	retryIn, err := p.w.runRetryScript(p.requestBp.Retry, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.Timeout.Duration)
	p.result.Retry = p.requestTimer.Time()
	p.retryIn = retryIn

	if err != nil {
		p.w.logger.Debug("Retry script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}

	return p.handleError(err)
}

func (p *RequestProcessor) run() error {
	for p.retryIn >= 0 {
		if p.retries > p.scenarioBp.Config.MaxRetries {
			err := errors.New("Too many retries")
			p.w.logger.Debug("Too many retries", p.w.loggingContext(p.ctx)...)
			return p.handleError(err)
		}
		p.retries++

		if err := p.send(); err != nil {
			return p.handleError(err)
		}

		if err := p.runRetry(); err != nil {
			return p.handleError(err)
		}

		if p.retryIn > 0 {
			timer := time.NewTimer(time.Duration(p.retryIn) * time.Millisecond)
			p.w.logger.Debug("Waiting for retry", append(p.w.loggingContext(p.ctx), "retryIn", p.retryIn)...)
			select {
			case <-timer.C:
				timer.Stop()
				continue
			case <-p.ctx.Done():
				*p.status = Aborted
				return errors.New("Context aborted while waiting for retry")
			}
		}
	}
	return nil
}

func (p *RequestProcessor) runAssert() error {
	p.w.logger.Debug("Worker running assert script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	err := p.w.runEffectfulScript(p.requestBp.Assert, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.Timeout.Duration)
	p.result.Assert = p.requestTimer.Time()

	if err != nil {
		p.w.logger.Debug("Assert script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}
	return p.handleError(err)
}

func (p *RequestProcessor) runPost() error {
	p.w.logger.Debug("Worker running post script for request", p.w.loggingContext(p.ctx)...)
	p.requestTimer.start()
	err := p.w.runEffectfulScript(p.requestBp.Post, p.requestBp.ExtractPayload, p.requestBp.Filename, p.scenarioBp.Config.Timeout.Duration)
	p.result.Post = p.requestTimer.Time()

	if err != nil {
		p.w.logger.Debug("Post script failed", p.w.loggingContextWithErr(p.ctx, err)...)
	}
	return p.handleError(err)
}

func (p *RequestProcessor) finalize() error {
	if p.result.Status == Aborted {
		p.result.Status = Success
	}
	p.result.Total = p.totalRequestTimer.Time()
	return nil
}

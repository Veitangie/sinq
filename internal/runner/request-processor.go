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

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/sinqerror"
	"veitangie.dev/sinq/internal/timer"
)

type RequestProcessor struct {
	w            *worker
	ctx          context.Context
	scenarioBp   *scenario.ScenarioBlueprint
	stageBp      *scenario.StageBlueprint
	stageIdx     int
	stageTimer   timer.Timer
	materialized []byte
	httpRequest  *http.Request
	requestBody  []byte
	multipart    multipartData
	reqTimeout   time.Duration
	retryIn      int64
	retries      int
	client       *http.Client
	filenameFrom string
	filenameTo   string
	cacheKey     string
	responseStr  string
	cache        bool
	skip         bool
}

func (p *RequestProcessor) runPre() error {
	if p.stageBp.Pre.IsEmpty() {
		return nil
	}

	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker running pre script for stage"))
	p.stageTimer.Start()
	res, err := p.w.runPreScript(
		p.stageBp.Pre,
		p.stageBp.ExtractPayload,
		p.stageBp.Filename,
		p.scenarioBp.Config.ScriptTimeout.Duration,
		p.stageBp.IsNoRequest(),
	)
	stage := p.w.stageEnd(p.ctx, event.PreScript, p.stageTimer.Time())

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Pre script failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage)
	}

	p.filenameFrom = res.filenameIn
	p.filenameTo = res.filenameOut
	p.cacheKey = res.cacheKey
	p.cache = res.cache
	p.skip = res.skip
	p.multipart = res.multipart
	return err
}

func (p *RequestProcessor) materialize() error {
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker materializing request"))
	p.stageTimer.Start()
	materialized, err := p.w.materializeRequest(p.ctx, p.stageBp, p.scenarioBp.Config.ScriptTimeout.Duration)
	p.materialized = materialized
	stage := p.w.stageEnd(p.ctx, event.Materialization, p.stageTimer.Time())

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Request materialization failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Request materialized successfully", slog.String("raw", string(materialized))))
	}

	return err
}

func (p *RequestProcessor) parse() error {
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker parsing request"))
	p.stageTimer.Start()
	parser, err := newParser(p.materialized, p.ctx)
	if err != nil {
		p.w.send(p.w.log(p.ctx, slog.LevelWarn, "[Runner] Failed to construct parser", event.ErrAttr(err)))
		return err
	}

	httpReq, body, err := parser.parse()
	stage := p.w.stageEnd(p.ctx, event.Parsing, p.stageTimer.Time())

	if err == nil && len(body) > 0 && p.filenameFrom != "" {
		err = fmt.Errorf("Request has both body and a file source")
	}

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Request parsing failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage)
	}

	p.httpRequest = &httpReq
	p.requestBody = body

	return err
}

func (p *RequestProcessor) processSendSuccess(result intermediate, execution event.Event) error {
	p.w.send(
		p.w.log(
			p.ctx,
			slog.LevelDebug,
			"[Runner] Extracted response",
			slog.Int("code", result.statusCode),
			slog.Any("headers", result.headers),
			slog.String("body", string(result.body)),
		),
		p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker capturing response"),
	)

	if len(result.body) > int(p.scenarioBp.Config.MaxBodySize.ByteAmount) {
		result.body = result.body[:p.scenarioBp.Config.MaxBodySize.ByteAmount]
		result.oversized = true
	}
	result.attempt = p.retries

	responseStr, err := p.w.requestCompleted(result)
	if p.w.env.cfg.DumpOnFailure {
		p.responseStr = responseStr
	}
	execution.Size = int(result.size)

	if p.cache {
		execution.Flags = execution.Flags | event.CachedMask
	}
	if result.oversized {
		execution.Flags = execution.Flags | event.OversizedMask
	}
	if result.fromCache {
		execution.Flags = execution.Flags | event.CacheHitMask
	}
	if result.leader {
		execution.Flags = execution.Flags | event.SingleflightLeaderMask
	}

	if err != nil {
		p.w.send(execution, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Failed to capture response", event.ErrAttr(err)))
		return err
	}
	p.w.send(execution)
	return nil
}

func (p *RequestProcessor) processSendError(attempt int, err error, execution event.Event) error {
	if err == nil {
		return nil
	}
	p.w.send(execution)

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if _, ok := errors.AsType[sinqerror.NetworkError](err); !ok {
		return err
	}

	p.w.send(
		p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker capturing response error"),
	)
	p.responseStr = ""
	errSave := p.w.requestCompletedErr(attempt, err)
	if errSave != nil {
		p.w.send(
			p.w.log(p.ctx, slog.LevelDebug, "[Runner] Failed to capture error", event.ErrAttr(errSave), slog.Attr{
				Key:   "requestError",
				Value: slog.StringValue(err.Error()),
			}),
		)
	}
	return errSave
}

func (p *RequestProcessor) run() error {
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Sending request"))

	var result intermediate
	var sendErr error
	for p.retryIn >= 0 {
		if p.retries > p.scenarioBp.Config.MaxRetries {
			p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Too many retries"))
			return errors.New("Too many retries")
		}
		p.retries++
		p.stageTimer.Start()

		if p.cache {
			result, sendErr = p.sendCached()
		} else {
			result, sendErr = p.send()
		}

		execution := p.w.stageEnd(p.ctx, event.Execution, p.stageTimer.Time())

		if sendErr != nil {
			if err := p.processSendError(p.retries, sendErr, execution); err != nil {
				return err
			}
		} else if err := p.processSendSuccess(result, execution); err != nil {
			return err
		}

		if err := p.runRetry(); err != nil {
			return err
		}

		if p.retryIn > 0 {
			timer := time.NewTimer(time.Duration(p.retryIn) * time.Millisecond)
			p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Waiting for retry", slog.Int64("retryIn", p.retryIn)))
			select {
			case <-timer.C:
				timer.Stop()
				continue
			case <-p.ctx.Done():
				timer.Stop()
				return p.ctx.Err()
			}
		}
	}
	return sendErr
}

func (p *RequestProcessor) getCacheKey() string {
	if p.cacheKey == "" {
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
		p.cacheKey = strconv.FormatUint(p.w.env.hasher.Sum64(), 16)
		p.w.env.hasher.Reset()
	}

	return p.cacheKey
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
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker sending request", slog.Int("attempt", p.retries)))

	if err := p.validateBody(); err != nil {
		return result, err
	}

	if err := p.materializeMultipart(); err != nil {
		return result, err
	}

	if p.filenameFrom != "" {
		file, err := p.w.env.workspace.Open(p.filenameFrom)

		if err != nil {
			p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Failed to open file for reading", event.ErrAttr(err), slog.String("filename", p.filenameFrom)))
			return result, err
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

	reqContext, cancel := context.WithTimeoutCause(p.ctx, p.reqTimeout, sinqerror.ErrRequestTimeout)
	defer cancel()

	p.stageTimer.Start()
	resp, err := p.client.Do(p.httpRequest.Clone(reqContext))

	if err != nil {
		err = sinqerror.WrapContextTimeoutErr(reqContext, err)
		p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Request execution failed", event.ErrAttr(err)))
		return result, fixClientErr(err)
	}

	res, err := makeIntermediate(p.w.env.workspace, p.filenameTo, int64(p.scenarioBp.Config.MaxBodySize.ByteAmount), resp)
	if err != nil {
		err = sinqerror.SwapContextTimeoutErr(reqContext, err)
		p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Request body read failed", event.ErrAttr(err)))
	}
	return res, err
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

	actualKey := p.getCacheKey() + "#" + strconv.Itoa(p.retries)

	if fromCache, err, ok := p.w.env.cachedProcessor.lookup(actualKey); ok {
		if err != nil {
			return result, err
		}
		result = fromCache
	} else {
		willBeSent := p.httpRequest.Clone(p.ctx)
		if p.client.Jar != nil {
			for _, cookie := range p.client.Jar.Cookies(willBeSent.URL) {
				willBeSent.AddCookie(cookie)
			}
		}

		p.stageTimer.Start()
		resCh := p.w.env.cachedProcessor.process(actualKey, *willBeSent, p.filenameTo, getBody, p.w.env.workspace, p.w.id)
		select {
		case res := <-resCh:
			if res.Err != nil {
				return result, res.Err
			}
			result = res.Val.(intermediate)
			result.leader = result.leaderToken == p.w.id

		case <-p.ctx.Done():
			return result, p.ctx.Err()
		}
	}

	if p.client.Jar != nil {
		setCookies := result.headers.Values("set-cookie")
		cookies := make([]*http.Cookie, 0, len(setCookies))

		for _, setCookie := range setCookies {
			cookie, err := http.ParseSetCookie(setCookie)
			if err != nil {
				p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Failed to set cookies", event.ErrAttr(err)))
			} else {
				cookies = append(cookies, cookie)
			}
		}

		p.client.Jar.SetCookies(p.httpRequest.URL, cookies)
	}

	return result, nil
}

func (p *RequestProcessor) runRetry() error {
	if p.stageBp.Retry.IsEmpty() {
		p.retryIn = -1
		return nil
	}
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker running retry script for stage"))
	p.stageTimer.Start()
	retryIn, err := p.w.runRetryScript(p.stageBp.Retry, p.stageBp.ExtractPayload, p.stageBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	stage := p.w.stageEnd(p.ctx, event.RetryScript, p.stageTimer.Time())
	p.retryIn = retryIn

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Retry script failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage)
	}

	return err
}

func (p *RequestProcessor) runAssert() error {
	if p.stageBp.Assert.IsEmpty() {
		return nil
	}
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker running assert script for stage"))
	p.stageTimer.Start()
	err := p.w.runAssertScript(p.stageBp.Assert, p.stageBp.ExtractPayload, p.stageBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration, p.filenameTo)
	stage := p.w.stageEnd(p.ctx, event.AssertScript, p.stageTimer.Time())

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Assert script failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage)
	}
	return err
}

func (p *RequestProcessor) runPost() error {
	if p.stageBp.Post.IsEmpty() {
		return nil
	}
	p.w.send(p.w.log(p.ctx, slog.LevelDebug, "[Runner] Worker running post script for stage"))
	p.stageTimer.Start()
	err := p.w.runEffectfulScript(p.stageBp.Post, p.stageBp.ExtractPayload, p.stageBp.Filename, p.scenarioBp.Config.ScriptTimeout.Duration)
	stage := p.w.stageEnd(p.ctx, event.PostScript, p.stageTimer.Time())

	if err != nil {
		p.w.send(stage, p.w.log(p.ctx, slog.LevelDebug, "[Runner] Post script failed", event.ErrAttr(err)))
	} else {
		p.w.send(stage)
	}
	return err
}

func (p *RequestProcessor) stageEnd(ctx context.Context, duration time.Duration) event.Event {
	e := p.w.stageEnd(ctx, event.StageEnd, duration)
	if p.w.env.cfg.DumpOnFailure {
		e.Request = string(p.materialized)
		e.Response = p.responseStr
	}
	return e
}

func (p *RequestProcessor) processError(ctx context.Context, err error, duration time.Duration) {
	if err == nil {
		return
	}

	status := event.Error
	end := p.stageEnd(ctx, duration)
	end.ErrorString = err.Error()
	if errors.Is(err, context.Canceled) {
		status = event.Aborted
		end.ErrorString = ""
	}
	if len(p.w.assertionFailures) > 0 {
		end.FailedAssertions = p.w.assertionFailures
		p.w.assertionFailures = make([]string, 0)
	}
	end.Status = status
	workerErr := p.w.newEvent(ctx, event.WorkerError)
	workerErr.Error = err
	p.w.send(end, workerErr, p.w.log(ctx, slog.LevelDebug, "[Runner] Stage failed", event.ErrAttr(err)))
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

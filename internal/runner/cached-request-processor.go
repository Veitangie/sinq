// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/sinqerror"
)

type intermediate struct {
	body        []byte
	headers     http.Header
	status      string
	proto       string
	filenameTo  string
	size        int64
	leaderToken int
	attempt     int
	statusCode  int
	oversized   bool
	fromCache   bool
	leader      bool
}

type cachedRequestProcessor struct {
	group        singleflight.Group
	ctx          context.Context
	transport    http.RoundTripper
	maxCacheSize config.DataSize
	cacheTimeout time.Duration
	eb           *event.Bus
	cache        sync.Map
}

func (sp *cachedRequestProcessor) log(level slog.Level, msg string, attrs ...slog.Attr) event.Event {
	return event.Event{
		Kind:    event.Log,
		Level:   level,
		Message: msg,
		Attrs:   attrs,
		Source:  event.Runner,
	}
}

func (sp *cachedRequestProcessor) lookup(hash string) (intermediate, error, bool) {
	if res, ok := sp.cache.Load(hash); ok {
		if resTyped, ok := res.(cachedResult[intermediate]); ok {
			return resTyped.res, resTyped.err, true
		}
	}
	return intermediate{}, nil, false
}

func (sp *cachedRequestProcessor) process(
	hash string,
	request http.Request,
	filenameTo string,
	getBody func(Workspace) func() (io.ReadCloser, error),
	workspace Workspace,
	leaderToken int,
) <-chan singleflight.Result {

	client := http.Client{
		Transport: sp.transport,
	}

	res := sp.group.DoChan(hash, singleflightSafe(func() (any, error) {
		if res, err, ok := sp.lookup(hash); ok {
			sp.eb.Send(
				context.Background(),
				sp.log(slog.LevelDebug, "[Runner] Found cached request, not running", slog.String("host", request.URL.Host), slog.String("path", request.URL.Path)),
			)
			return res, err
		}

		sp.eb.Send(
			context.Background(),
			sp.log(slog.LevelDebug, "[Runner] Running cached http request for the first time", slog.String("host", request.URL.Host), slog.String("path", request.URL.Path)),
		)
		request.GetBody = getBody(workspace)
		body, err := getBody(workspace)()
		if err != nil {
			sp.eb.Send(context.Background(), sp.log(slog.LevelDebug, "[Runner] Failed to open request body", event.ErrAttr(err)))
			return intermediate{}, err
		}

		request.Body = body
		betterContext, cancel := context.WithTimeoutCause(sp.ctx, sp.cacheTimeout, sinqerror.ErrRequestTimeout)
		defer cancel()
		betterRequest := request.WithContext(betterContext)

		resp, err := client.Do(betterRequest)
		if err != nil {
			err = sinqerror.WrapContextTimeoutErr(betterContext, err)
			return intermediate{}, fixClientErr(err)
		}

		res, err := makeIntermediate(workspace, filenameTo, int64(sp.maxCacheSize.ByteAmount), resp)
		res.leaderToken = leaderToken
		cachedRes := res
		cachedRes.fromCache = true
		err = sinqerror.SwapContextTimeoutErr(betterContext, err)
		sp.cache.Store(hash, cachedResult[intermediate]{res: cachedRes, err: err})
		return res, err
	}))

	return res
}

func singleflightSafe(underlying func() (any, error)) func() (any, error) {
	return func() (result any, err error) {
		defer func() {
			panickedWith := recover()
			if panickedWith != nil {
				result = nil
				err = fmt.Errorf("Caught a panic in single flight: %v", panickedWith)
			}
		}()
		result, err = underlying()
		return
	}
}

func makeIntermediate(workspace Workspace, filenameTo string, maxBodySize int64, response *http.Response) (intermediate, error) {
	defer response.Body.Close()
	result := intermediate{
		statusCode: response.StatusCode,
		status:     response.Status,
		proto:      response.Proto,
		headers:    response.Header,
	}

	if filenameTo != "" {
		file, err := workspace.Create(filenameTo)
		if err != nil {
			return result, err
		}
		defer file.Close()

		written, err := io.Copy(file, wrappingReader{response.Body})
		if err != nil {
			return result, err
		}
		result.filenameTo = filenameTo
		result.size = written
		return result, nil
	}

	limited := io.LimitReader(response.Body, maxBodySize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return result, sinqerror.WrapNetworkError(err)
	}

	result.size = min(int64(len(data)), maxBodySize)
	if int64(len(data)) > maxBodySize {
		data = data[:len(data)-1]
		result.oversized = true
		io.CopyN(io.Discard, response.Body, 50*(1<<20))
	}

	result.body = data
	return result, nil
}

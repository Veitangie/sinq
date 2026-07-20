// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"golang.org/x/sync/singleflight"
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

type cachedRequestProcessor struct {
	group        singleflight.Group
	ctx          context.Context
	transport    http.RoundTripper
	maxCacheSize config.DataSize
	cacheTimeout time.Duration
	logger       *slog.Logger
	cache        sync.Map
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
) <-chan singleflight.Result {

	client := http.Client{
		Transport: sp.transport,
		Timeout:   sp.cacheTimeout,
	}

	res := sp.group.DoChan(hash, func() (any, error) {
		if res, err, ok := sp.lookup(hash); ok {
			sp.logger.Debug("[Runner] Found cached request, not running", "host", request.URL.Host, "path", request.URL.Path)
			return res, err
		}

		sp.logger.Debug("[Runner] Running cached http request for the first time", "host", request.URL.Host, "path", request.URL.Path)

		betterContext, cancel := context.WithTimeout(sp.ctx, sp.cacheTimeout)
		defer cancel()
		betterRequest := request.WithContext(betterContext)
		betterRequest.GetBody = getBody(workspace)

		resp, err := client.Do(betterRequest)
		if err != nil {
			return intermediate{}, err
		}
		defer resp.Body.Close()

		res, err := makeIntermediate(workspace, filenameTo, int64(sp.maxCacheSize.ByteAmount), resp)
		sp.cache.Store(hash, cachedResult[intermediate]{res: res, err: err})
		return res, err
	})

	return res
}

func makeIntermediate(workspace Workspace, filenameTo string, maxBodySize int64, response *http.Response) (intermediate, error) {
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

		written, err := io.Copy(file, response.Body)
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
		return result, err
	}

	if uint64(len(data)) > uint64(maxBodySize) {
		data = data[:len(data)-1]
		io.CopyN(io.Discard, response.Body, 50*(1<<20))
		result.oversized = true
	}

	result.body = data
	return result, nil
}

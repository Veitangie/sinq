// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"fmt"
	"hash/maphash"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
)

func TestWorker_ProcessStage_ExecutionCarriesCacheFlags(t *testing.T) {
	raw := `### First
GET http://localhost/same HTTP/1.1

$PRE{ req.cache("k") }

### Second
GET http://localhost/same HTTP/1.1

$PRE{ req.cache("k") }

### Uncached
GET http://localhost/other HTTP/1.1
`
	_, obs := runSource(t, raw, cachedVia)

	ex := executions(obs)
	if len(ex) != 3 {
		t.Fatalf("Expected 3 Execution events, got %d", len(ex))
	}
	if !hasFlag(ex[0], event.CachedMask) || hasFlag(ex[0], event.CacheHitMask) || !hasFlag(ex[0], event.SingleflightLeaderMask) {
		t.Errorf("Expected the first cached request to be flagged Cached and Leader but not a hit, got flags %b", ex[0].Flags)
	}
	if !hasFlag(ex[1], event.CachedMask) || !hasFlag(ex[1], event.CacheHitMask) || hasFlag(ex[1], event.SingleflightLeaderMask) {
		t.Errorf("Expected the second request with the same key to be flagged Cached and CacheHit but not Leader, got flags %b", ex[1].Flags)
	}
	if ex[2].Flags != 0 {
		t.Errorf("Expected an uncached request to carry no cache flags, got %b", ex[2].Flags)
	}
}

func TestWorker_ProcessStage_CacheFlagsPerRetryAttempt(t *testing.T) {
	raw := `### First
GET http://localhost/poll HTTP/1.1

$PRE{ req.cache("p") }
$RETRY{ if res.attempt < 2 then return 0 end; return -1 }

### Second
GET http://localhost/poll HTTP/1.1

$PRE{ req.cache("p") }
$RETRY{ if res.attempt < 2 then return 0 end; return -1 }
`
	_, obs := runSource(t, raw, cachedVia)

	ex := executions(obs)
	if len(ex) != 4 {
		t.Fatalf("Expected 2 attempts per request (4 Execution events), got %d", len(ex))
	}
	for _, e := range ex {
		if !hasFlag(e, event.CachedMask) {
			t.Errorf("Request %d: expected every attempt of a cached request to be flagged Cached, got %b", e.StageIdx, e.Flags)
		}
		if hit := hasFlag(e, event.CacheHitMask); hit != (e.StageIdx == 1) {
			t.Errorf("Request %d: expected CacheHit only on the second request's attempts, got %b", e.StageIdx, e.Flags)
		}
	}
}

func TestWorker_ProcessStage_ExecutionFlagsOversized(t *testing.T) {
	cases := []struct {
		name  string
		limit uint64
		want  bool
	}{
		{"body over the limit", 3, true},
		{"body exactly at the limit", 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, obs := runSourceWith(t, "GET http://localhost/ HTTP/1.1\n",
				func(cfg *scenario.ScenarioConfig) { cfg.MaxBodySize.ByteAmount = tc.limit },
				func(w *worker) { w.env.transport = bodyRoundTripper{body: "hello"} })

			ex := executions(obs)
			if len(ex) != 1 {
				t.Fatalf("Expected one Execution event, got %d", len(ex))
			}
			if got := hasFlag(ex[0], event.OversizedMask); got != tc.want {
				t.Errorf("Expected Oversized flag %v for a 5 byte body with limit %d, got flags %b", tc.want, tc.limit, ex[0].Flags)
			}
			if wantSize := min(5, int(tc.limit)); ex[0].Size != wantSize {
				t.Errorf("Expected Size to be the %d bytes kept, got %d", wantSize, ex[0].Size)
			}
			if hasFlag(ex[0], event.CachedMask) || hasFlag(ex[0], event.CacheHitMask) {
				t.Errorf("Expected no cache flags on an uncached request, got %b", ex[0].Flags)
			}
		})
	}
}

func TestWorker_ProcessStage_SameWorkerRepeatIsHitNotLeader(t *testing.T) {
	raw := `### Loop
GET http://localhost/same HTTP/1.1

$PRE{ req.cache("same") }
$POST{ passes = (passes or 0) + 1; if passes < 3 then sinq.setNextStage(1) end }
`
	w := setupTestWorker(t, nil)
	transport := &countingRoundTripper{}
	w.env.transport = transport
	cachedVia(w)
	obs := observe(t, w)

	run := func() {
		bps, err := scenario.ParseStageBlueprints(strings.NewReader(raw), "same.sinq", "")
		if err != nil {
			t.Fatalf("Failed to parse requests: %v", err)
		}
		w.processScenario(context.Background(), taskBundle{
			Config: &scenario.ScenarioConfig{
				Name:          "Same",
				ReqTimeout:    scenario.Duration{Duration: time.Second},
				ScriptTimeout: scenario.Duration{Duration: time.Second},
				MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
			},
			Stages:    bps,
			workspace: &mockWorkspace{},
			run:       true,
		})
	}
	run()
	run()

	ex := executions(obs)
	if len(ex) != 6 {
		t.Fatalf("Expected 3 passes in each of 2 runs (6 Execution events), got %d", len(ex))
	}
	if n := transport.sent.Load(); n != 1 {
		t.Errorf("Expected only the first pass to reach the network, got %d requests", n)
	}
	for i, e := range ex {
		leader := hasFlag(e, event.SingleflightLeaderMask)
		hit := hasFlag(e, event.CacheHitMask)
		if i == 0 && (!leader || hit) {
			t.Errorf("Execution %d: expected the pass that sent to be Leader and not a hit, got %b", i, e.Flags)
		}
		if i > 0 && (leader || !hit) {
			t.Errorf("Execution %d: expected a repeat on the same worker to be a hit, not Leader, got %b", i, e.Flags)
		}
	}
}

func TestWorker_ProcessStage_InFlightFollowerFlags(t *testing.T) {
	transport := &heldRoundTripper{arrived: make(chan struct{}), release: make(chan struct{})}
	raw := "GET http://localhost/shared HTTP/1.1\n\n$PRE{ req.cache(\"shared\") }\n"

	leader, follower := setupTestWorker(t, nil), setupTestWorker(t, nil)
	follower.id = 2
	follower.env.cachedProcessor = leader.env.cachedProcessor
	leader.env.cachedProcessor.transport = transport
	leaderObs, followerObs := observe(t, leader), observe(t, follower)

	bundle := func() taskBundle {
		bps, err := scenario.ParseStageBlueprints(strings.NewReader(raw), "shared.sinq", "")
		if err != nil {
			t.Fatalf("Failed to parse requests: %v", err)
		}
		return taskBundle{
			Config: &scenario.ScenarioConfig{
				Name:          "Shared",
				ReqTimeout:    scenario.Duration{Duration: 5 * time.Second},
				ScriptTimeout: scenario.Duration{Duration: time.Second},
				MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
			},
			Stages:    bps,
			workspace: &mockWorkspace{},
			run:       true,
		}
	}

	var wg sync.WaitGroup
	wg.Go(func() { leader.processScenario(context.Background(), bundle()) })
	select {
	case <-transport.arrived:
	case <-time.After(observeTimeout):
		t.Fatal("The leader's request never reached the transport")
	}
	wg.Go(func() { follower.processScenario(context.Background(), bundle()) })
	followerObs.awaitLogged("[Runner] Sending request")
	time.Sleep(10 * time.Millisecond)
	close(transport.release)
	wg.Wait()

	if n := transport.sent.Load(); n != 1 {
		t.Fatalf("Expected the follower to share the leader's call (1 request sent), got %d", n)
	}
	l, f := executions(leaderObs), executions(followerObs)
	if len(l) != 1 || len(f) != 1 {
		t.Fatalf("Expected one Execution per worker, got %d and %d", len(l), len(f))
	}
	if !hasFlag(l[0], event.CachedMask) || !hasFlag(l[0], event.SingleflightLeaderMask) || hasFlag(l[0], event.CacheHitMask) {
		t.Errorf("Expected the leader to be flagged Cached and Leader, not a hit, got %b", l[0].Flags)
	}
	if !hasFlag(f[0], event.CachedMask) || hasFlag(f[0], event.SingleflightLeaderMask) || hasFlag(f[0], event.CacheHitMask) {
		t.Errorf("Expected the follower to be flagged Cached only, got %b", f[0].Flags)
	}
}

func sharedCacheWorkers(t *testing.T, n int, transport http.RoundTripper) []*worker {
	t.Helper()
	seed := maphash.MakeSeed()
	workers := make([]*worker, n)
	for i := range workers {
		w := setupTestWorker(t, nil)
		w.id = i + 1
		w.env.hasher.SetSeed(seed)
		w.env.transport = transport
		if i > 0 {
			w.env.cachedProcessor = workers[0].env.cachedProcessor
		}
		workers[i] = w
	}
	workers[0].env.cachedProcessor.transport = transport
	return workers
}

func countingServer(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		time.Sleep(delay)
		w.Write([]byte("success"))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func TestWorker_ProcessStage_SingleFlight(t *testing.T) {
	cases := []struct {
		name  string
		raw   func(srv string, i int) string
		sends int32
	}{
		{"identical cached requests collapse", func(srv string, i int) string {
			return "GET " + srv + "/ HTTP/1.1\n\n$PRE{ req.cache(true) }\n"
		}, 1},
		{"distinct cached requests don't", func(srv string, i int) string {
			return "GET " + srv + "/ HTTP/1.1\nX-Unique-ID: " + fmt.Sprint(i) + "\n\n$PRE{ req.cache(true) }\n"
		}, 10},
		{"uncached requests don't", func(srv string, i int) string {
			return "GET " + srv + "/ HTTP/1.1\n"
		}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, count := countingServer(t, 50*time.Millisecond)
			workers := sharedCacheWorkers(t, 10, srv.Client().Transport)
			observers := make([]*observer, len(workers))
			for i, w := range workers {
				observers[i] = observe(t, w)
			}

			var wg sync.WaitGroup
			for i, w := range workers {
				bundle := eventsBundle(t, tc.raw(srv.URL, i), nil)
				wg.Go(func() { w.processScenario(context.Background(), bundle) })
			}
			wg.Wait()

			if n := count.Load(); n != tc.sends {
				t.Errorf("Expected the server to be hit %d times, got %d", tc.sends, n)
			}
			for i, obs := range observers {
				if rr := obs.result().StageResults[0]; rr.Status != event.Success {
					t.Errorf("Worker %d: expected Success, got %v (%s)", i+1, rr.Status, rr.ErrorMessage)
				}
			}
		})
	}
}

func TestWorker_ProcessStage_SingleFlightFollowerKeepsItsOwnDeadline(t *testing.T) {
	srv, _ := countingServer(t, 150*time.Millisecond)
	workers := sharedCacheWorkers(t, 2, srv.Client().Transport)
	short, long := observe(t, workers[0]), observe(t, workers[1])
	raw := "GET " + srv.URL + "/ HTTP/1.1\n\n$PRE{ req.cache(true) }\n$ASSERT{ sinq.assert.equals(res.bodyRaw, \"success\") }\n"

	var wg sync.WaitGroup
	wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		workers[0].processScenario(ctx, eventsBundle(t, raw, nil))
	})
	wg.Go(func() {
		workers[1].processScenario(context.Background(), eventsBundle(t, raw, nil))
	})
	wg.Wait()

	if rr := short.result().StageResults[0]; rr.Status != event.Error || !strings.Contains(rr.ErrorMessage, "deadline exceeded") {
		t.Errorf("Expected the worker whose scenario timed out to error, got %v %q", rr.Status, rr.ErrorMessage)
	}
	if rr := long.result().StageResults[0]; rr.Status != event.Success {
		t.Errorf("Expected the other worker to still get the shared response, got %v %v %s", rr.Status, rr.FailedAssertions, rr.ErrorMessage)
	}
}

func TestWorker_ProcessStage_CacheHitStillSetsCookies(t *testing.T) {
	var logins atomic.Int32
	var mu sync.Mutex
	var cookieHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			w.Header().Add("Set-Cookie", "session_id=12345; Path=/")
			w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		case "/check":
			mu.Lock()
			cookieHeaders = append(cookieHeaders, r.Header.Get("Cookie"))
			mu.Unlock()
		}
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.env.transport = srv.Client().Transport
	cachedVia(w)
	obs := observe(t, w)
	raw := "### Login\nGET " + srv.URL + "/login HTTP/1.1\n\n$PRE{ req.cache(true) }\n\n### Check\nGET " + srv.URL + "/check HTTP/1.1\n"

	for range 2 {
		w.processScenario(context.Background(), eventsBundle(t, raw, nil))
		if res := obs.result(); res.Status != event.Success {
			t.Fatalf("Expected the scenario to succeed, got %v", res.Status)
		}
	}

	if n := logins.Load(); n != 1 {
		t.Errorf("Expected the second run's login to come from the cache, got %d logins", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cookieHeaders) != 2 {
		t.Fatalf("Expected two checks, got %d", len(cookieHeaders))
	}
	for i, h := range cookieHeaders {
		if !strings.Contains(h, "session_id=12345") || !strings.Contains(h, "theme=dark") {
			t.Errorf("Run %d: expected the login's cookies on the next request, got %q", i+1, h)
		}
	}
}

func TestWorker_ProcessStage_CacheKeyAndAttemptDontCollide(t *testing.T) {
	raw := `### First
GET http://localhost/first HTTP/1.1

$PRE{ req.cache("k1") }

### Second
GET http://localhost/second HTTP/1.1

$PRE{ req.cache("k") }
$RETRY{ if res.attempt < 11 then return 0 end; return -1 }
`
	_, obs := runSourceWith(t, raw, func(cfg *scenario.ScenarioConfig) { cfg.MaxRetries = 11 }, cachedVia)

	ex := executions(obs)
	if len(ex) != 12 {
		t.Fatalf("Expected 1 + 11 Execution events, got %d", len(ex))
	}
	if last := ex[len(ex)-1]; hasFlag(last, event.CacheHitMask) {
		t.Errorf("Expected attempt 11 of key \"k\" not to hit the entry of key \"k1\" attempt 1, got flags %b", last.Flags)
	}
}

func TestWorker_ProcessStage_ExecutionCarriesSize(t *testing.T) {
	_, obs := runSource(t, "GET http://localhost/ HTTP/1.1\n", func(w *worker) { w.env.transport = bodyRoundTripper{body: "hello"} })

	ex := executions(obs)
	if len(ex) != 1 {
		t.Fatalf("Expected one Execution event, got %d", len(ex))
	}
	if ex[0].Size != 5 {
		t.Errorf("Expected Size to be the response body length (5), got %d", ex[0].Size)
	}
}

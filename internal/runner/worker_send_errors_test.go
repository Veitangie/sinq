// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
)

func TestWorker_ProcessRequest_FailedSendIsAnAttempt(t *testing.T) {
	res, obs := runSource(t, "GET http://localhost/ HTTP/1.1\n", func(w *worker) { w.env.transport = refusingRoundTripper{} })

	if rr := res.RequestResults[0]; rr.Status != event.Error {
		t.Fatalf("Expected the request to error, got %v", rr.Status)
	}
	if ex := executions(obs); len(ex) != 1 {
		t.Errorf("Expected one Execution event for the failed send, got %d", len(ex))
	}
	if rr := res.RequestResults[0]; rr.Attempts != 1 {
		t.Errorf("Expected the failed send to count as 1 attempt, got %d", rr.Attempts)
	}
}

func TestWorker_ProcessRequest_TooManyRetries(t *testing.T) {
	transport := &countingRoundTripper{}
	res, _ := runSourceWith(t, "GET http://localhost/ HTTP/1.1\n\n$RETRY{ return 1 }\n",
		func(cfg *scenario.ScenarioConfig) { cfg.MaxRetries = 2 },
		viaTransport(transport))

	rr := res.RequestResults[0]
	if rr.Status != event.Error || rr.ErrorMessage != "Too many retries" {
		t.Errorf("Expected the request to end with \"Too many retries\", got %v %q", rr.Status, rr.ErrorMessage)
	}
	if n := transport.sent.Load(); n != 3 {
		t.Errorf("Expected the first attempt plus max_retries (2) retries, 3 sends, got %d", n)
	}
}

// An unsupported scheme is a mistake in the request, not a network failure, so it
// must end the request instead of going through $RETRY.
func TestWorker_ProcessRequest_UnsupportedSchemeIsNotRetried(t *testing.T) {
	res, _ := runSourceWith(t, "GET ftp://localhost/x HTTP/1.1\n\n$RETRY{ return 1 }\n",
		func(cfg *scenario.ScenarioConfig) { cfg.MaxRetries = 2 },
		viaTransport(http.DefaultTransport))

	rr := res.RequestResults[0]
	if rr.Status != event.Error || !strings.Contains(strings.ToLower(rr.ErrorMessage), "scheme") {
		t.Errorf("Expected the request to error over its scheme, got %v %q", rr.Status, rr.ErrorMessage)
	}
	if rr.Attempts > 1 {
		t.Errorf("Expected no retries for an unsupported scheme, got %d attempts", rr.Attempts)
	}
}

func TestWorker_ProcessRequest_SendErrorHidesURLCredentials(t *testing.T) {
	res, _ := runSource(t, "GET http://alice:hunter2@localhost/x?token=s3cret HTTP/1.1\n", viaTransport(refusingRoundTripper{}))

	msg := res.RequestResults[0].ErrorMessage
	if !strings.Contains(msg, "localhost/x") {
		t.Fatalf("Expected the error to still name the URL, got %q", msg)
	}
	for _, secret := range []string{"alice", "hunter2", "s3cret"} {
		if strings.Contains(msg, secret) {
			t.Errorf("Expected %q to be stripped from the error, got %q", secret, msg)
		}
	}
}

func TestWorker_ProcessRequest_RetriesSendJarCookiesOnce(t *testing.T) {
	tests := []struct {
		name string
		pre  string
	}{
		{name: "plain request"},
		{name: "cached request", pre: "$PRE{ req.cache(true) }\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := make(chan string, 8)
			srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					http.SetCookie(rw, &http.Cookie{Name: "sid", Value: "1"})
					return
				}
				seen <- r.Header.Get("Cookie")
			}))
			defer srv.Close()

			raw := "### Login\nGET " + srv.URL + "/login HTTP/1.1\n\n" +
				"### Poll\nGET " + srv.URL + "/poll HTTP/1.1\n\n" + tt.pre + "$RETRY{ if res.attempt < 3 then return 0 end; return -1 }\n"
			runSource(t, raw, viaTransport(http.DefaultTransport), cachedVia)
			close(seen)

			attempts := 0
			for cookie := range seen {
				attempts++
				if cookie != "sid=1" {
					t.Errorf("Attempt %d: expected the Cookie header to be \"sid=1\", got %q", attempts, cookie)
				}
			}
			if attempts != 3 {
				t.Errorf("Expected 3 attempts to reach the server, got %d", attempts)
			}
		})
	}
}

func TestWorker_ProcessRequest_UntrustedCertificateErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)

	res, _ := runSource(t, "GET "+srv.URL+"/path HTTP/1.1\n", viaTransport(http.DefaultTransport))

	rr := res.RequestResults[0]
	if rr.Status != event.Error || !strings.Contains(rr.ErrorMessage, "certificate") {
		t.Errorf("Expected a self-signed server to fail with a certificate error, got %v %q", rr.Status, rr.ErrorMessage)
	}
}

func TestWorker_ProcessScenario_CancelDuringRetryWaitAborts(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.transport = &countingRoundTripper{}
	obs := observe(t, w)
	bundle := eventsBundle(t, "GET http://localhost/ HTTP/1.1\n\n$RETRY{ return 10000 }\n", func(cfg *scenario.ScenarioConfig) {
		cfg.ReqTimeout = scenario.Duration{Duration: 30 * time.Second}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.processScenario(ctx, bundle)
		close(done)
	}()
	obs.awaitLogged("[Runner] Waiting for retry")
	cancel()

	select {
	case <-done:
	case <-time.After(observeTimeout):
		t.Fatal("The retry wait ignored the cancellation")
	}
	res := obs.result()
	if rr := res.RequestResults[0]; rr.Status != event.Aborted || rr.ErrorMessage != "" {
		t.Errorf("Expected a request cancelled in its retry wait to be Aborted without a message, got %v %q", rr.Status, rr.ErrorMessage)
	}
	if res.Status != event.Aborted {
		t.Errorf("Expected the scenario to be Aborted, got %v", res.Status)
	}
}

func TestWorker_ProcessRequest_FailedRetryAttemptClearsPreviousResponse(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n" +
		"$RETRY{\n" +
		"  if res.attempt < 2 then return 0 end\n" +
		"  print(\"code=\" .. tostring(res.code) .. \" body=\" .. tostring(res.bodyRaw) .. \" error=\" .. tostring(res.error))\n" +
		"  return -1\n" +
		"}\n"
	res, _ := runSource(t, raw, func(w *worker) {
		w.env.transport = &okThenRefusingRoundTripper{}
		w.env.cfg.Print = true
	})

	if res.Output == nil {
		t.Fatal("Expected $RETRY's print output to be captured")
	}
	out := res.Output.String()
	if !strings.Contains(out, "error=") || strings.Contains(out, "error=nil") {
		t.Fatalf("Expected the second attempt's send error on res.error, got %q", out)
	}
	if !strings.Contains(out, "code=nil") || !strings.Contains(out, "body=nil") {
		t.Errorf("Expected a failed attempt to leave no code or body from the previous attempt, got %q", out)
	}
}

func TestWorker_ProcessRequest_ErrorTableToString(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n" +
		"$RETRY{\n" +
		"  print(\"type=\" .. type(res.error))\n" +
		"  print(\"str=\" .. tostring(res.error))\n" +
		"  res.error.message = \"edited\"\n" +
		"  print(res.error)\n" +
		"  return -1\n" +
		"}\n"
	res, _ := runSource(t, raw, func(w *worker) {
		w.env.transport = refusingRoundTripper{}
		w.env.cfg.Print = true
	})

	if res.Output == nil {
		t.Fatal("Expected $RETRY's print output to be captured")
	}
	out := res.Output.String()
	if !strings.Contains(out, "type=table\n") {
		t.Errorf("Expected res.error to be a table, got %q", out)
	}
	if !strings.Contains(out, "str=other: Get \"http://localhost/\": connection refused\n") {
		t.Errorf("Expected tostring(res.error) to be \"kind: message\", got %q", out)
	}
	if !strings.Contains(out, "\nother: edited\n") {
		t.Errorf("Expected print to use the edited message, got %q", out)
	}
}

func TestWorker_ProcessRequest_SuccessfulRetryClearsError(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n" +
		"$RETRY{\n" +
		"  print(\"attempt=\" .. res.attempt .. \" error=\" .. tostring(res.error) .. \" code=\" .. tostring(res.code))\n" +
		"  if res.error then return 0 end\n" +
		"  return -1\n" +
		"}\n" +
		"$ASSERT{ sinq.assert.code(200) }\n"
	res, _ := runSource(t, raw, func(w *worker) {
		w.env.transport = &refusingThenOkRoundTripper{}
		w.env.cfg.Print = true
	})

	if res.Output == nil {
		t.Fatal("Expected $RETRY's print output to be captured")
	}
	out := res.Output.String()
	if !strings.Contains(out, "attempt=2 error=nil code=200") {
		t.Errorf("Expected the successful second attempt to clear res.error, got %q", out)
	}
	if rr := res.RequestResults[0]; rr.Status != event.Success {
		t.Errorf("Expected the request to succeed on its retry, got %v (%s)", rr.Status, rr.ErrorMessage)
	}
}

func TestWorker_ProcessRequest_AuthoringErrorInSendStillErrors(t *testing.T) {
	raw := "$PRE{ req.multipart({ name = \"a\", data = \"b\" }) }\n" +
		"POST http://localhost/ HTTP/1.1\n\nliteral body\n\n" +
		"$ASSERT{ sinq.assert.isTrue(true) }\n"
	res, _ := runSource(t, raw)

	rr := res.RequestResults[0]
	if rr.Status != event.Error {
		t.Errorf("Expected a request with two bodies to error, got %v", rr.Status)
	}
	if !strings.Contains(rr.ErrorMessage, "more than one body") {
		t.Errorf("Expected the two-bodies error message, got %q", rr.ErrorMessage)
	}
}

func TestWorker_ProcessRequest_RequestTimeoutReachesRetry(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n$RETRY{ print(\"error=\" .. tostring(res.error.message) .. \" kind=\" .. tostring(res.error.kind)); return -1 }\n"
	res, _ := runSourceWith(t, raw, func(cfg *scenario.ScenarioConfig) {
		cfg.ReqTimeout = scenario.Duration{Duration: 20 * time.Millisecond}
	}, func(w *worker) {
		w.env.transport = hangingRoundTripper{}
		w.env.cfg.Print = true
	})

	if res.Output == nil || !strings.Contains(res.Output.String(), "error=request timeout kind=timeout") {
		got := "<no output>"
		if res.Output != nil {
			got = res.Output.String()
		}
		t.Errorf("Expected $RETRY to see the request timeout on res.error, got %q", got)
	}
	if rr := res.RequestResults[0]; rr.Status != event.Error {
		t.Errorf("Expected a request whose last attempt timed out to error, got %v", rr.Status)
	}
}

const printErrorKindRetry = "GET http://localhost/ HTTP/1.1\n\n$RETRY{ print(\"kind=\" .. tostring(res.error and res.error.kind)); return -1 }\n"

func TestWorker_ProcessRequest_ResetWhileReadingBodyReachesRetry(t *testing.T) {
	res, _ := runSource(t, printErrorKindRetry, func(w *worker) {
		w.env.transport = bodyFailingRoundTripper{failErr: io.ErrUnexpectedEOF}
		w.env.cfg.Print = true
	})

	if res.Output == nil || !strings.Contains(res.Output.String(), "kind=reset") {
		got := "<no output>"
		if res.Output != nil {
			got = res.Output.String()
		}
		t.Errorf("Expected $RETRY to see a mid-body reset as kind reset, got %q", got)
	}
}

func TestWorker_ProcessRequest_TimeoutWhileReadingBodyReachesRetry(t *testing.T) {
	res, _ := runSourceWith(t, printErrorKindRetry, func(cfg *scenario.ScenarioConfig) {
		cfg.ReqTimeout = scenario.Duration{Duration: 20 * time.Millisecond}
	}, func(w *worker) {
		w.env.transport = bodyFailingRoundTripper{}
		w.env.cfg.Print = true
	})

	if res.Output == nil || !strings.Contains(res.Output.String(), "kind=timeout") {
		got := "<no output>"
		if res.Output != nil {
			got = res.Output.String()
		}
		t.Errorf("Expected $RETRY to see a body read timeout as kind timeout, got %q", got)
	}
	if rr := res.RequestResults[0]; !strings.Contains(rr.ErrorMessage, "request timeout") {
		t.Errorf("Expected the request to end with the request timeout, got %v %q", rr.Status, rr.ErrorMessage)
	}
}

func TestWorker_ProcessRequest_SaveToFileFailureIsNotANetworkError(t *testing.T) {
	raw := "$PRE{ req.saveResponseTo(\"out/report.bin\") }\n" +
		"GET http://localhost/ HTTP/1.1\n\n" +
		"$RETRY{ if res.error then return 0 end; return -1 }\n"
	w := setupTestWorker(t, nil)
	w.env.transport = bodyRoundTripper{body: "payload"}
	obs := observe(t, w)

	bps, err := scenario.ParseRequestBlueprints(strings.NewReader(raw), "events.sinq")
	if err != nil {
		t.Fatalf("Failed to parse requests: %v", err)
	}
	w.processScenario(context.Background(), taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:          "Events",
			ReqTimeout:    scenario.Duration{Duration: time.Second},
			ScriptTimeout: scenario.Duration{Duration: time.Second},
			MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
			MaxRetries:    3,
		},
		Requests:  bps,
		workspace: &uncreatableWorkspace{mockWorkspace{FS: fstest.MapFS{"out": &fstest.MapFile{Mode: fs.ModeDir}}}},
		run:       true,
	})
	rr := obs.result().RequestResults[0]

	if rr.Status != event.Error {
		t.Errorf("Expected the request to error, got %v", rr.Status)
	}
	if !strings.Contains(rr.ErrorMessage, "permission denied") {
		t.Errorf("Expected the file creation error, got %q", rr.ErrorMessage)
	}
	if rr.Attempts != 1 {
		t.Errorf("Expected no retries of a filesystem error, got %d attempts", rr.Attempts)
	}
}

func TestWorker_ProcessRequest_ScenarioTimeoutDoesNotReachRetry(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.transport = hangingRoundTripper{}
	w.env.cfg.Print = true
	obs := observe(t, w)

	bps, err := scenario.ParseRequestBlueprints(strings.NewReader("GET http://localhost/ HTTP/1.1\n\n$RETRY{ print(\"retry ran\"); return -1 }\n"), "events.sinq")
	if err != nil {
		t.Fatalf("Failed to parse requests: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w.processScenario(ctx, taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:          "Events",
			ReqTimeout:    scenario.Duration{Duration: 5 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: time.Second},
			MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
		},
		Requests:  bps,
		workspace: &mockWorkspace{},
		run:       true,
	})
	res := obs.result()

	if res.Output != nil && strings.Contains(res.Output.String(), "retry ran") {
		t.Errorf("Expected $RETRY not to run when the scenario timed out, got output %q", res.Output.String())
	}
	rr := res.RequestResults[0]
	if rr.Status != event.Error {
		t.Errorf("Expected a request cut off by the scenario timeout to error, got %v", rr.Status)
	}
	if strings.Contains(rr.ErrorMessage, "request timeout") {
		t.Errorf("Expected the scenario deadline, not a request timeout, got %q", rr.ErrorMessage)
	}
}

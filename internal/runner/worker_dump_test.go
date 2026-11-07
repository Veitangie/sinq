// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"strings"
	"testing"

	"veitangie.dev/sinq/internal/event"
)

const failingAssertWithPost = `GET http://localhost/ HTTP/1.1

$ASSERT{ sinq.assert.fail("boom") }
$POST{ ran = true }
`

func TestWorker_ProcessRequest_FailedAssertionEndsPassOnce(t *testing.T) {
	res, obs := runSource(t, failingAssertWithPost)

	ends := requestEndsFor(obs, 0)
	if len(ends) != 1 {
		t.Fatalf("Expected one RequestEnd for one pass, got %d: %+v", len(ends), ends)
	}
	if ends[0].Status != event.Failure {
		t.Errorf("Expected the pass to end as Failure, got %v", ends[0].Status)
	}
	if rr := res.RequestResults[0]; rr.Total != ends[0].Duration {
		t.Errorf("Expected Total to be the one pass's duration %v, got %v", ends[0].Duration, rr.Total)
	}
}

func TestWorker_ProcessRequest_FailedAssertionKeepsDump(t *testing.T) {
	res, _ := runSource(t, failingAssertWithPost, func(w *worker) { w.env.cfg.DumpOnFailure = true })

	if rr := res.RequestResults[0]; rr.Request == "" || rr.Response == "" {
		t.Errorf("Expected a dump for the failed request with --dump-on-failure, got request %q and response %q", rr.Request, rr.Response)
	}
}

func TestWorker_ProcessRequest_NoDumpWithoutDumpOnFailure(t *testing.T) {
	res, _ := runSourceWith(t, failingAssertWithPost, failFast)

	if rr := res.RequestResults[0]; rr.Request != "" || rr.Response != "" {
		t.Errorf("Expected no dump without --dump-on-failure, got request %q and response %q", rr.Request, rr.Response)
	}
}

func TestWorker_ProcessRequest_DumpedRequestEndsWithNewline(t *testing.T) {
	res, _ := runSource(t, "$ASSERT{ sinq.assert.fail(\"boom\") }\nPOST http://localhost/ HTTP/1.1\n\nbody",
		func(w *worker) { w.env.cfg.DumpOnFailure = true })

	rr := res.RequestResults[0]
	if rr.Status != event.Failure {
		t.Fatalf("Expected the assertion to fail the request, got %v: %s", rr.Status, rr.ErrorMessage)
	}
	if !strings.HasSuffix(rr.Request, "body\n") {
		t.Errorf("Expected the dumped request to end with a newline, got %q", rr.Request)
	}
}

func TestWorker_ProcessRequest_FailFastAssertionKeepsDump(t *testing.T) {
	res, _ := runSourceWith(t, failingAssertWithPost, failFast, func(w *worker) { w.env.cfg.DumpOnFailure = true })

	rr := res.RequestResults[0]
	if rr.Status != event.Failure {
		t.Fatalf("Expected the assertion to fail the request, got %v", rr.Status)
	}
	if rr.Request == "" || rr.Response == "" {
		t.Errorf("Expected a dump for a fail-fast assertion failure, got request %q and response %q", rr.Request, rr.Response)
	}
}

func TestWorker_ProcessRequest_PostErrorKeepsFailedAssertions(t *testing.T) {
	res, _ := runSource(t, "GET http://localhost/ HTTP/1.1\n\n$ASSERT{ sinq.assert.fail(\"boom\") }\n$POST{ error(\"post broke\") }\n")

	rr := res.RequestResults[0]
	if rr.Status != event.Error {
		t.Errorf("Expected the $POST error to make the request Error, got %v", rr.Status)
	}
	if len(rr.FailedAssertions) != 1 || rr.FailedAssertions[0] != "boom" {
		t.Errorf("Expected the failed assertion to be kept alongside the $POST error, got %v", rr.FailedAssertions)
	}
}

func TestWorker_ProcessRequest_FailedSendKeepsRequestDump(t *testing.T) {
	res, _ := runSource(t, "GET http://localhost/ HTTP/1.1\n", func(w *worker) {
		w.env.transport = refusingRoundTripper{}
		w.env.cfg.DumpOnFailure = true
	})

	rr := res.RequestResults[0]
	if rr.Status != event.Error {
		t.Fatalf("Expected the refused request to error, got %v", rr.Status)
	}
	if !strings.Contains(rr.Request, "GET http://localhost/") {
		t.Errorf("Expected the request dump for a request whose send failed, got %q", rr.Request)
	}
}

func TestWorker_ProcessRequest_ParseFailureKeepsRequestDump(t *testing.T) {
	res, obs := runSource(t, "NOT A VALID REQUEST LINE\n", func(w *worker) { w.env.cfg.DumpOnFailure = true })

	rr := res.RequestResults[0]
	if rr.Status != event.Error {
		t.Fatalf("Expected the unparseable request to error, got %v: %s", rr.Status, rr.ErrorMessage)
	}
	if len(executions(obs)) != 0 {
		t.Fatalf("Expected the request to fail before sending, but it reached Execution")
	}
	if !strings.Contains(rr.Request, "NOT A VALID REQUEST LINE") {
		t.Errorf("Expected the request dump for a request that failed to parse, got %q", rr.Request)
	}
}

func TestWorker_ProcessRequest_DumpWithinPassIsFromLastAttempt(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n$RETRY{ if res.attempt < 2 then return 0 end; return -1 }\n$ASSERT{ sinq.assert.fail(\"boom\") }\n"
	res, _ := runSource(t, raw, func(w *worker) {
		w.env.transport = &sequenceRoundTripper{bodies: []string{"first attempt", "last attempt"}}
		w.env.cfg.DumpOnFailure = true
	})

	rr := res.RequestResults[0]
	if rr.Attempts != 2 {
		t.Fatalf("Expected 2 attempts, got %d", rr.Attempts)
	}
	if !strings.Contains(rr.Response, "last attempt") || strings.Contains(rr.Response, "first attempt") {
		t.Errorf("Expected the dump to show the last attempt's response, got %q", rr.Response)
	}
}

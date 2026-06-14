// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSinq_EndToEnd_ComplexAsyncPolling(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/login":
			fmt.Fprint(w, `{"token": "secret-jwt-123"}`)

		case "/jobs":
			if r.Header.Get("Authorization") != "Bearer secret-jwt-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"job_id": 42}`)

		case "/jobs/42":
			mu.Lock()
			defer mu.Unlock()
			pollCount++
			if pollCount < 3 {
				fmt.Fprint(w, `{"status": "pending"}`)
			} else {
				fmt.Fprint(w, `{"status": "complete"}`)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()

	configData := fmt.Sprintf(`{"env": {"BASE_URL": "%s"}, "max_retries": 5}`, srv.URL)
	_ = os.WriteFile(filepath.Join(tmpDir, "config.scenario"), []byte(configData), 0644)

	req1 := `POST ${env.BASE_URL}/login
Accept: application/json

$POST{
	if sinq.responses[1].code ~= 200 then 
		sinq.test.fail("Login failed") 
	end
	-- Leak token into the global sandbox for subsequent requests
	_G.AUTH_TOKEN = sinq.responses[1].json().token
}`
	_ = os.WriteFile(filepath.Join(tmpDir, "01_login.sinq"), []byte(req1), 0644)

	req2 := `$PRE{
	if not _G.AUTH_TOKEN then error("Token did not carry over") end
	-- Prepare dynamic payload
	_G.PAYLOAD = '{"action": "export"}'
}
POST ${env.BASE_URL}/jobs
Authorization: Bearer ${ _G.AUTH_TOKEN }

${ _G.PAYLOAD }

$POST{
	if sinq.responses[2].code ~= 202 then 
		sinq.test.fail("Failed to start job") 
	end
	_G.JOB_ID = sinq.responses[2].json().job_id
}`
	_ = os.WriteFile(filepath.Join(tmpDir, "02_start_job.sinq"), []byte(req2), 0644)

	req3 := `GET ${env.BASE_URL}/jobs/$Unnamed#1{ _G.JOB_ID }

$RETRY{
	-- Wait for the job to complete
	local status = sinq.responses[3].json().status
	if status == "pending" then
		return 10 -- sleep 10ms and retry
	end
	return -1 -- Stop retrying, proceed to Assert
}

$ASSERT{
	if sinq.responses[3].json().status ~= "complete" then
		sinq.test.fail("Job never completed")
	end
}`
	_ = os.WriteFile(filepath.Join(tmpDir, "03_poll_job.sinq"), []byte(req3), 0644)

	args := []string{
		"--color", "always",
		"--workers", "6",
		"--format", "default",
		tmpDir,
	}

	exitCode := sinq(args)

	if exitCode != 0 {
		t.Fatalf("Expected sinq to exit cleanly, but got code %d. E2E execution failed.", exitCode)
	}

	mu.Lock()
	finalPollCount := pollCount
	mu.Unlock()

	if finalPollCount != 3 {
		t.Errorf("Expected the poller to hit the server exactly 3 times due to $RETRY, but hit it %d times", finalPollCount)
	}
}

func TestSinq_EndToEnd_Chaos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blackhole":
			time.Sleep(250 * time.Millisecond)
		case "/crash":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error": "database melted"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()

	timeoutDir := filepath.Join(tmpDir, "01_timeout")
	os.MkdirAll(timeoutDir, 0755)

	_ = os.WriteFile(filepath.Join(timeoutDir, "config.scenario"), []byte(`{"timeout": "50ms"}`), 0644)

	req1 := fmt.Sprintf("GET %s/blackhole\n", srv.URL)
	_ = os.WriteFile(filepath.Join(timeoutDir, "blackhole.sinq"), []byte(req1), 0644)

	assertDir := filepath.Join(tmpDir, "02_assertion")
	os.MkdirAll(assertDir, 0755)

	req2 := fmt.Sprintf(`GET %s/crash

$ASSERT{
	if sinq.responses[1].code == 500 then
		sinq.test.fail("Caught the expected 500, failing the test mathematically.")
	end
}`, srv.URL)
	_ = os.WriteFile(filepath.Join(assertDir, "crash.sinq"), []byte(req2), 0644)

	panicDir := filepath.Join(tmpDir, "03_panic")
	os.MkdirAll(panicDir, 0755)

	req3 := fmt.Sprintf(`GET %s/404

$POST{
	-- Accessing an index that doesn't exist, then calling a property on it
	-- This will cause a hard Lua panic.
	local bad_data = sinq.responses[999].body.does_not_exist
}`, srv.URL)
	_ = os.WriteFile(filepath.Join(panicDir, "panic.sinq"), []byte(req3), 0644)

	args := []string{
		"--workers", "3",
		"--format", "default",
		tmpDir,
	}

	exitCode := sinq(args)

	if exitCode == 0 {
		t.Fatalf("Chaos test exited with code 0! The runner swallowed fatal errors instead of failing the CI pipeline.")
	}
}
func TestSinq_EndToEnd_Stress(t *testing.T) {
	var requestCount atomic.Uint64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	oldStderr := os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		os.Stderr = devNull
		defer devNull.Close()
	}
	defer func() { os.Stderr = oldStderr }()

	tmpDir := t.TempDir()
	numScenarios := 1000

	for i := range numScenarios {
		fileName := filepath.Join(tmpDir, fmt.Sprintf("req_%04d.sinq", i))

		content := fmt.Sprintf("GET %s/ping\n$ASSERT{ assert(sinq.responses[1].code == 200) }", srv.URL)

		err := os.WriteFile(fileName, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write scenario %d: %v", i, err)
		}
	}

	args := []string{
		"--workers", "1000",
		"--format", "default",
		tmpDir,
	}

	exitCode := sinq(args)

	if exitCode != 0 {
		t.Fatalf("Stress test exited with code %d! 1,000 workers crushed the engine.", exitCode)
	}

	if requestCount.Load() != uint64(numScenarios) {
		t.Fatalf("Expected %d requests to hit the mock server, but got %d", numScenarios, requestCount.Load())
	}
}

func TestSinq_CLI_BasicFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{"Help Flag", []string{"--help"}, 0},
		{"Version Flag", []string{"--version"}, 0},
		{"List Flag", []string{"--list"}, 0},
		{"Invalid Flag", []string{"--this-does-not-exist"}, 1},
		{"Missing Secrets Path", []string{"--secrets", "/path/to/nowhere.json", "."}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout, oldStderr := os.Stdout, os.Stderr
			devNull, _ := os.Open(os.DevNull)
			os.Stdout, os.Stderr = devNull, devNull
			defer func() {
				os.Stdout, os.Stderr = oldStdout, oldStderr
				devNull.Close()
			}()

			if got := sinq(tt.args); got != tt.wantCode {
				t.Errorf("sinq(%v) = %d; want %d", tt.args, got, tt.wantCode)
			}
		})
	}
}

func TestSinq_CLI_OutputFormatters(t *testing.T) {
	tmpDir := t.TempDir()
	outFilePath := filepath.Join(tmpDir, "report.xml")

	req := `GET http://localhost:9999/ping`
	_ = os.WriteFile(filepath.Join(tmpDir, "dummy.sinq"), []byte(req), 0644)

	args := []string{
		"--out", outFilePath,
		"--format", "junit",
		tmpDir,
	}

	oldStdout, oldStderr := os.Stdout, os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stdout, os.Stderr = devNull, devNull
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		devNull.Close()
	}()

	sinq(args)

	if _, err := os.Stat(outFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected --out flag to create file at %s, but it was not found", outFilePath)
	}
}

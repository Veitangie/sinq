// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/spinq"
)

func withCapturedIO(t *testing.T, out, err io.Writer) {
	t.Helper()
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &lockedWriter{w: out}, spinq.WriterPassthrough{Writer: &lockedWriter{w: err}}
	t.Cleanup(func() {
		stdout, stderr = oldOut, oldErr
	})
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

type erroringWriter struct{ err error }

func (w erroringWriter) Write(p []byte) (int, error) { return 0, w.err }

type flakyWriter struct {
	failAfter int
	calls     int
	err       error
}

func (w *flakyWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.failAfter {
		return 0, w.err
	}
	return len(p), nil
}

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
		"--format", "std",
		tmpDir,
	}

	withCapturedIO(t, io.Discard, io.Discard)
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

	_ = os.WriteFile(filepath.Join(timeoutDir, "config.scenario"), []byte(`{"req_timeout": "50ms"}`), 0644)

	req1 := fmt.Sprintf("GET %s/blackhole\n", srv.URL)
	_ = os.WriteFile(filepath.Join(timeoutDir, "blackhole.sinq"), []byte(req1), 0644)

	assertDir := filepath.Join(tmpDir, "02_assertion")
	os.MkdirAll(assertDir, 0755)

	req2 := fmt.Sprintf(`GET %s/crash

$ASSERT{
	if sinq.responses[1].code == 500 then
		sinq.assert.fail("Caught the expected 500, failing the test mathematically.")
	end
}`, srv.URL)
	_ = os.WriteFile(filepath.Join(assertDir, "crash.sinq"), []byte(req2), 0644)

	panicDir := filepath.Join(tmpDir, "03_panic")
	os.MkdirAll(panicDir, 0755)

	req3 := fmt.Sprintf(`GET %s/404

$POST{
	local bad_data = sinq.responses[999].body.does_not_exist
}`, srv.URL)
	_ = os.WriteFile(filepath.Join(panicDir, "panic.sinq"), []byte(req3), 0644)

	args := []string{
		"--workers", "3",
		"--format", "std",
		tmpDir,
	}

	withCapturedIO(t, io.Discard, io.Discard)
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

	withCapturedIO(t, io.Discard, io.Discard)

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
		"--format", "std",
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
		{"Invalid Flag", []string{"--this-does-not-exist"}, codeSetupError},
		{"Missing Secrets Path", []string{"--secrets-file", "/path/to/nowhere.json", "."}, codeSetupError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCapturedIO(t, io.Discard, io.Discard)

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

	withCapturedIO(t, io.Discard, io.Discard)
	sinq(args)

	if _, err := os.Stat(outFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected --out flag to create file at %s, but it was not found", outFilePath)
	}
}

func TestSinq_CLI_OutFileKeepsFormatStderrGetsStd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFilePath := filepath.Join(tmpDir, "out", "report.xml")
	_ = os.WriteFile(filepath.Join(tmpDir, "ping.sinq"), []byte("GET "+srv.URL+"/ping\n"), 0644)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	withCapturedIO(t, stdout, stderr)
	if code := sinq([]string{"--no-spinner", "-C", "never", "-f", "junit", "-o", outFilePath, tmpDir}); code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	data, err := os.ReadFile(outFilePath)
	if err != nil {
		t.Fatalf("Expected the report file, got %v", err)
	}
	if !strings.Contains(string(data), "<testsuite ") {
		t.Errorf("Expected a JUnit report in the file, got:\n%s", data)
	}
	if strings.Contains(stderr.String(), "<testsuite") || strings.Contains(stderr.String(), "<?xml") {
		t.Errorf("Expected no XML on stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr.String(), "PASSED") || !strings.Contains(stderr.String(), "1 request sent") {
		t.Errorf("Expected the std summary on stderr, got:\n%s", stderr)
	}
	if stdout.Len() != 0 {
		t.Errorf("Expected nothing on stdout with --out, got:\n%s", stdout)
	}
}

func TestSinq_CLI_ListScenarios(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tmpDir, "config.scenario"), []byte(`{"tags": ["api"], "env_matrix": [{"key1": {"a": "1"}}, {"key2": {"b": "2"}}]}`), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "01_test.sinq"), []byte("GET http://example.com"), 0644)

	args1 := []string{"--list", "--tag", "api", tmpDir}
	args2 := []string{"--list", "--tag", "nonexistent", tmpDir}

	withCapturedIO(t, io.Discard, io.Discard)

	if got := sinq(args1); got != 0 {
		t.Errorf("sinq(--list --tag api) exited with %d; want 0", got)
	}

	if got := sinq(args2); got != 0 {
		t.Errorf("sinq(--list --tag nonexistent) exited with %d; want 0", got)
	}
}

func TestSinq_CLI_HandleReporting_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()

	req := fmt.Sprintf("GET %s\n$ASSERT{ sinq.assert.code(200) }", srv.URL)
	_ = os.WriteFile(filepath.Join(tmpDir, "fail.sinq"), []byte(req), 0644)

	args := []string{
		"--workers", "1",
		"--format", "std",
		tmpDir,
	}

	withCapturedIO(t, io.Discard, io.Discard)

	if got := sinq(args); got != 1 {
		t.Errorf("sinq() with failing test exited with %d; want 1", got)
	}
}

func TestSinq_CLI_CreateReporter_InvalidFormat(t *testing.T) {
	cfg := config.Config{Format: "unknown"}
	reporter := createReporter(cfg, os.Stdout, false)
	if reporter == nil {
		t.Errorf("Expected createReporter to fallback to standard reporter, got nil")
	}
}

func TestPopulateConfigInRuntime(t *testing.T) {
	os.Setenv("SINQ_LUA_PATH", "/tmp/luapath1"+string(os.PathListSeparator)+"/tmp/luapath2")
	defer os.Unsetenv("SINQ_LUA_PATH")

	cfg := &config.Config{}
	_, err := populateConfigInRuntime(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Paths) != 1 || cfg.Paths[0] != "." {
		t.Errorf("expected Paths to be ['.'], got %v", cfg.Paths)
	}

	if len(cfg.LuaPaths) != 3 {
		t.Errorf("expected 3 LuaPaths, got %d", len(cfg.LuaPaths))
	}
}

func TestPopulateConfigInRuntime_WithPaths(t *testing.T) {
	cfg := &config.Config{
		Paths:    []string{"/tmp/testpath"},
		LuaPaths: []string{"/tmp/existinglua"},
	}
	_, err := populateConfigInRuntime(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Paths) != 1 || cfg.Paths[0] != "/tmp/testpath" {
		t.Errorf("expected Paths to be ['/tmp/testpath'], got %v", cfg.Paths)
	}

	if len(cfg.LuaPaths) != 3 {
		t.Errorf("expected 3 LuaPaths, got %d", len(cfg.LuaPaths))
	}
}

func TestSinq_EndToEnd_SingleFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	req := fmt.Sprintf("GET %s/\n$ASSERT{ assert(sinq.responses[1].code == 200) }", srv.URL)
	filePath := filepath.Join(tmpDir, "single_test.sinq")
	_ = os.WriteFile(filePath, []byte(req), 0644)

	args := []string{
		"--format", "std",
		filePath,
	}

	withCapturedIO(t, io.Discard, io.Discard)

	exitCode := sinq(args)
	if exitCode != 0 {
		t.Fatalf("Expected sinq to successfully execute a single file, but got exit code %d. The engine likely failed because it tried to treat the file as a directory.", exitCode)
	}
}

func TestSinq_EndToEnd_SingleFile_ParseFailure(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "bad.sinq")
	_ = os.WriteFile(filePath, []byte("$PRE{ "), 0644)

	args := []string{
		"--format", "std",
		filePath,
	}

	withCapturedIO(t, io.Discard, io.Discard)

	exitCode := sinq(args)
	if exitCode == 0 {
		t.Fatalf("BUG FOUND: Expected sinq to fail and exit with non-zero code on a malformed single file, but got 0. It likely swallowed the error!")
	}
}

func TestSinq_EndToEnd_StupidFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.sinq")

	req := fmt.Sprintf("### my / stupid test\nGET %s/\n$PRE{ req.saveResponseTo('out') }", srv.URL)
	_ = os.WriteFile(filePath, []byte(req), 0644)

	args := []string{
		"--format", "std",
		filePath,
	}

	out := &bytes.Buffer{}
	withCapturedIO(t, out, out)
	exitCode := sinq(args)
	if exitCode != 0 {
		t.Fatalf("BUG FOUND: Expected sinq to succeed even with slashes in the request name, but got exit code %d:\n%s", exitCode, out)
	}
}

func TestOSWorkspaceMkDirall(t *testing.T) {
	ws, err := NewOSRootWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}

	err = ws.MkDirall("sinq_mkdir_test")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	ws2 := OSWorkspace{}
	err = ws2.MkDirall(filepath.Join(os.TempDir(), "sinq_scenario_mkdir_test"))
	if err != nil {
		t.Errorf("Unexpected error in OSWorkspace: %v", err)
	}
}

func TestInTermWantColor_NilFile(t *testing.T) {
	got := wantColor(nil, config.SaneDefaults())
	if got {
		t.Error("expected wantColor to be false for a nil file")
	}
}

func TestSinq_NoSpinner_DoesNotPanic(t *testing.T) {
	if os.Getenv("SINQ_TEST_NO_SPINNER_SUBPROCESS") == "1" {
		tmpDir, err := os.MkdirTemp("", "sinq_no_spinner_subprocess")
		if err != nil {
			fmt.Fprintln(os.Stderr, "setup error:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "01_test.sinq"), []byte("GET http://example.com"), 0644); err != nil {
			fmt.Fprintln(os.Stderr, "setup error:", err)
			os.Exit(2)
		}

		withCapturedIO(t, &bytes.Buffer{}, &bytes.Buffer{})
		sinq([]string{"--no-spinner", "--list", tmpDir})
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSinq_NoSpinner_DoesNotPanic")
	cmd.Env = append(os.Environ(), "SINQ_TEST_NO_SPINNER_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sinq() with --no-spinner crashed in a subprocess: %v\noutput:\n%s", err, out)
	}
}

func TestSinq_NonexistentPath(t *testing.T) {
	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	code := sinq([]string{"--no-spinner", "/this/path/does/not/exist/at/all"})
	if code != codeSetupError {
		t.Errorf("expected exit code %d for a nonexistent path, got %d", codeSetupError, code)
	}
	if !strings.Contains(errBuf.String(), "/this/path/does/not/exist/at/all: ") {
		t.Errorf("expected the error to lead with the missing path, got %q", errBuf.String())
	}
}

func TestSinq_HelpAndVersion_PrintError(t *testing.T) {
	tests := [][]string{{"--help"}, {"--version"}}
	for _, args := range tests {
		t.Run(strings.Join(args, ""), func(t *testing.T) {
			withCapturedIO(t, &erroringWriter{err: errors.New("broken pipe")}, &bytes.Buffer{})
			if code := sinq(args); code != codeSetupError {
				t.Errorf("sinq(%v) = %d; want %d when stdout fails to write", args, code, codeSetupError)
			}
		})
	}
}

func TestSinq_List_PrintScenariosError(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "01_test.sinq"), []byte("GET http://example.com"), 0644)

	withCapturedIO(t, &erroringWriter{err: errors.New("broken pipe")}, &bytes.Buffer{})

	if code := sinq([]string{"--no-spinner", "--list", tmpDir}); code != codeSetupError {
		t.Errorf("sinq(--list) = %d; want %d when stdout fails to write", code, codeSetupError)
	}
}

func TestSinq_CIWarnings(t *testing.T) {
	t.Setenv("CI", "true")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	req := fmt.Sprintf("GET %s\n$ASSERT{ sinq.assert.code(200) }", srv.URL)
	_ = os.WriteFile(filepath.Join(tmpDir, "ok.sinq"), []byte(req), 0644)

	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	code := sinq([]string{"--no-spinner", "--log-level", "debug", "--dump-on-failure", "--workers", "1", tmpDir})
	if code != 0 {
		t.Fatalf("expected sinq() to succeed, got exit code %d: %s", code, errBuf.String())
	}

	if !strings.Contains(errBuf.String(), "--log-level debug") {
		t.Errorf("expected a CI warning about --log-level debug, got %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "--dump-on-failure") {
		t.Errorf("expected a CI warning about --dump-on-failure, got %q", errBuf.String())
	}
}

func TestSinq_OutFlag_PointsAtDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "01_test.sinq"), []byte("GET "+srv.URL+"\n"), 0644)

	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	code := sinq([]string{"--no-spinner", "--out", tmpDir, tmpDir})
	if code != codeSetupError {
		t.Errorf("expected exit code %d when --out points at a directory, got %d", codeSetupError, code)
	}
	if !strings.Contains(errBuf.String(), "Failed to open output file") {
		t.Errorf("expected an 'output file' error, got %q", errBuf.String())
	}
}

func TestSinq_ErrorChannel_IsDrainedAndReported(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "config.scenario"), []byte(`{not valid json`), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "01_test.sinq"), []byte("GET http://example.com"), 0644)

	valid := filepath.Join(tmpDir, "valid")
	_ = os.Mkdir(valid, 0755)
	_ = os.WriteFile(filepath.Join(valid, "02_test.sinq"), []byte("GET http://example.com"), 0644)

	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	sinq([]string{"--no-spinner", "--workers", "1", tmpDir})

	if !strings.Contains(errBuf.String(), "Error:") {
		t.Errorf("expected the malformed config.scenario error to be reported, got %q", errBuf.String())
	}
}

func TestHandleCompletion_UnrecognizedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("handleCompletion always succeeds on windows via the ps1 fallback")
	}

	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	code := sinq([]string{"--completion"})
	if code != codeSuccess && code != codeSetupError {
		t.Errorf("expected exit code %d or %d from --completion, got %d", codeSuccess, codeSetupError, code)
	}
}

func writeRepeatedStageScenario(t *testing.T) (dir string, sent *atomic.Int32) {
	t.Helper()
	sent = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	dir = t.TempDir()
	req := fmt.Sprintf(`### Setup
GET %[1]s/setup
$POST{ items = { "a", "b", "c" } }

### Loop
GET %[1]s/loop?id=${ return items[1] }
$ASSERT{ sinq.assert.isTrue(items[1] ~= "a", "iteration for a failed") }
$POST{ table.remove(items, 1); if #items > 0 then sinq.setNextStage(#sinq.responses) end }

### After
GET %[1]s/after
`, srv.URL)
	_ = os.WriteFile(filepath.Join(dir, "repeat.sinq"), []byte(req), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config.scenario"), []byte(`{"name": "repeat", "fail_fast": false}`), 0644)
	return dir, sent
}

func TestSinq_EndToEnd_RepeatedStage_StdReportTallies(t *testing.T) {
	dir, sent := writeRepeatedStageScenario(t)

	out := &bytes.Buffer{}
	withCapturedIO(t, out, io.Discard)

	if exitCode := sinq([]string{"--no-spinner", "--format", "std", dir}); exitCode == 0 {
		t.Fatalf("Expected a non-zero exit code for a failed scenario\n%s", out.String())
	}
	if sent.Load() != 5 {
		t.Fatalf("Expected 5 requests on the wire (Setup, Loop x3, After), got %d\n%s", sent.Load(), out.String())
	}

	report := out.String()
	if !strings.Contains(report, "| 5 requests sent") {
		t.Errorf("Expected the summary to count every pass of the repeated stage (5 requests sent)\n%s", report)
	}
	for line := range strings.Lines(report) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "✓ Loop") {
			t.Errorf("Expected the Loop stage to be marked failed, since one of its passes failed: %q\n%s", trimmed, report)
		}
		if strings.Contains(trimmed, "Scenario: repeat") && !strings.Contains(trimmed, "2✓ 1✗") {
			t.Errorf("Expected the scenario tally to show 2✓ 1✗ (Setup and After passed, Loop failed): %q\n%s", trimmed, report)
		}
	}
}

func TestSinq_EndToEnd_RepeatedStage_JUnitReportsFailure(t *testing.T) {
	dir, _ := writeRepeatedStageScenario(t)
	outFile := filepath.Join(t.TempDir(), "report.xml")

	withCapturedIO(t, io.Discard, io.Discard)
	sinq([]string{"--no-spinner", "--format", "junit", "--out", outFile, dir})

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read JUnit report: %v", err)
	}
	report := string(data)

	if !strings.Contains(report, `<testsuite name="repeat" tests="3" failures="1" errors="0"`) {
		t.Errorf("Expected the suite to count 3 stages with 1 failure\n%s", report)
	}
	loopStart := strings.Index(report, `<testcase name="Loop"`)
	if loopStart < 0 {
		t.Fatalf("Expected a Loop testcase\n%s", report)
	}
	loopEnd := strings.Index(report[loopStart:], "</testcase>")
	if loopEnd < 0 || !strings.Contains(report[loopStart:loopStart+loopEnd], `<failure message="iteration for a failed"`) {
		t.Errorf("Expected the Loop testcase to carry the failure from its first pass\n%s", report)
	}
}

func TestSinq_EndToEnd_CountJUnitReportIsValid(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "probe.sinq"), fmt.Appendf(nil, "GET %s/probe\n$ASSERT{ sinq.assert.code(200) }\n", srv.URL), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config.scenario"), []byte(`{"name": "count", "tags": ["smoke"]}`), 0644)
	outFile := filepath.Join(t.TempDir(), "report.xml")

	withCapturedIO(t, io.Discard, io.Discard)
	if exitCode := sinq([]string{"--no-spinner", "--count", "3", "--format", "junit", "--out", outFile, dir}); exitCode != 0 {
		t.Fatalf("Expected a passing run, got exit code %d", exitCode)
	}
	if sent.Load() != 3 {
		t.Fatalf("Expected 3 requests for --count 3, got %d", sent.Load())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read JUnit report: %v", err)
	}

	var report struct {
		XMLName xml.Name `xml:"testsuites"`
		Tests   int      `xml:"tests,attr"`
		Suites  []struct {
			Name       string `xml:"name,attr"`
			Tests      int    `xml:"tests,attr"`
			Properties []struct {
				Property []struct {
					Name  string `xml:"name,attr"`
					Value string `xml:"value,attr"`
				} `xml:"property"`
			} `xml:"properties"`
			TestCases []struct {
				Name string `xml:"name,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(data, &report); err != nil {
		t.Fatalf("Expected --count to produce valid JUnit XML: %v\n%s", err, data)
	}

	if report.Tests != 3 || len(report.Suites) != 3 {
		t.Fatalf("Expected 3 tests in 3 suites, one per run, got %d tests in %d suites\n%s", report.Tests, len(report.Suites), data)
	}

	names := map[string]bool{}
	for i, suite := range report.Suites {
		names[suite.Name] = true
		if suite.Tests != 1 || len(suite.TestCases) != 1 || suite.TestCases[0].Name != "probe.sinq" {
			t.Errorf("Expected suite %d to be a full copy of the scenario, got %+v", i, suite)
		}
		if len(suite.Properties) != 1 || len(suite.Properties[0].Property) != 1 ||
			suite.Properties[0].Property[0].Name != "tags" || suite.Properties[0].Property[0].Value != "smoke" {
			t.Errorf("Expected suite %d to carry the tags property, got %+v", i, suite.Properties)
		}
	}
	for _, want := range []string{"count#1", "count#2", "count#3"} {
		if !names[want] {
			t.Errorf("Expected a suite named %q, got %v", want, names)
		}
	}
}

func TestSinq_EndToEnd_FileMatches(t *testing.T) {
	const chunk = 32 << 10
	big := bytes.Repeat([]byte("0123456789abcdef"), 5*chunk/16)
	bigChanged := bytes.Clone(big)
	bigChanged[len(bigChanged)-1] = 'X'

	cases := []struct {
		name     string
		served   []byte
		expected []byte
		match    bool
	}{
		{"same content", []byte("hello"), []byte("hello"), true},
		{"both empty", []byte{}, []byte{}, true},
		{"same length, different content", []byte("hello"), []byte("hellO"), false},
		{"response shorter", []byte("hell"), []byte("hello"), false},
		{"response longer", []byte("hello!"), []byte("hello"), false},
		{"response empty", []byte{}, []byte("hello"), false},
		{"several chunks, same", big, big, true},
		{"several chunks, last byte differs", big, bigChanged, false},
		{"exactly one chunk vs one byte more", big[:chunk], big[:chunk+1], false},
		{"one byte more vs exactly one chunk", big[:chunk+1], big[:chunk], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(tc.served)
			}))
			defer srv.Close()

			dir := t.TempDir()
			_ = os.WriteFile(filepath.Join(dir, "expected.bin"), tc.expected, 0644)
			_ = os.WriteFile(filepath.Join(dir, "download.sinq"), fmt.Appendf(nil,
				"$PRE{ req.saveResponseTo(\"out/got.bin\") }\nGET %s/file\n\n$ASSERT{ sinq.assert.fileMatches(\"expected.bin\") }\n", srv.URL), 0644)

			out := &bytes.Buffer{}
			withCapturedIO(t, out, io.Discard)
			exitCode := sinq([]string{"--no-spinner", "--color", "never", dir})

			if tc.match && exitCode != 0 {
				t.Errorf("Expected matching files to pass, got exit code %d:\n%s", exitCode, out)
			}
			if !tc.match && (exitCode == 0 || !strings.Contains(out.String(), "Saved response and file did not match")) {
				t.Errorf("Expected different files to fail the assertion, got exit code %d:\n%s", exitCode, out)
			}
		})
	}
}

func TestSinq_EndToEnd_RootScenarioIsNamedAfterItsFolder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "probe.sinq"), fmt.Appendf(nil, "GET %s/probe\n$ASSERT{ sinq.assert.code(200) }\n", srv.URL), 0644)

	suiteNames := func(t *testing.T, path string) []string {
		t.Helper()
		outFile := filepath.Join(t.TempDir(), "report.xml")
		withCapturedIO(t, io.Discard, io.Discard)
		if exitCode := sinq([]string{"--no-spinner", "--format", "junit", "--out", outFile, path}); exitCode != 0 {
			t.Fatalf("Expected a passing run for %q, got exit code %d", path, exitCode)
		}
		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("Failed to read JUnit report: %v", err)
		}
		var report struct {
			Suites []struct {
				Name string `xml:"name,attr"`
			} `xml:"testsuite"`
		}
		if err := xml.Unmarshal(data, &report); err != nil {
			t.Fatalf("Invalid JUnit XML: %v\n%s", err, data)
		}
		names := []string{}
		for _, s := range report.Suites {
			names = append(names, s.Name)
		}
		return names
	}

	cases := []struct {
		name  string
		chdir bool
		path  string
		want  string
	}{
		{"absolute path", false, dir, "checkout"},
		{"trailing slash", false, dir + string(filepath.Separator), "checkout"},
		{"dot from inside the folder", true, ".", "checkout"},
		{"dot slash from inside the folder", true, "." + string(filepath.Separator), "checkout"},
		{"parent-relative path", true, filepath.Join("..", "checkout"), "checkout"},
		{"single file by bare name", true, "probe.sinq", "probe.sinq"},
		{"single file by relative path", true, filepath.Join("..", "checkout", "probe.sinq"), "probe.sinq"},
		{"single file by absolute path", false, filepath.Join(dir, "probe.sinq"), "probe.sinq"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.chdir {
				t.Chdir(dir)
			}
			if got := suiteNames(t, tc.path); len(got) != 1 || got[0] != tc.want {
				t.Errorf("Expected one scenario named %q for path %q, got %v", tc.want, tc.path, got)
			}
		})
	}
}

func TestSinq_CountZeroIsRejected(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "probe.sinq"), fmt.Appendf(nil, "GET %s/probe\n", srv.URL), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config.scenario"), []byte(`{"name": "probe"}`), 0644)

	tests := []struct {
		name string
		args []string
	}{
		{name: "alone", args: []string{"--no-spinner", "--count", "0", dir}},
		{name: "with a filter that excludes the scenario", args: []string{"--no-spinner", "--count", "0", "--name", "nomatch", dir}},
		{name: "with a filter that includes the scenario", args: []string{"--no-spinner", "--count", "0", "--name", "probe", dir}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent.Store(0)
			out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
			withCapturedIO(t, out, errBuf)

			if code := sinq(tt.args); code == 0 {
				t.Errorf("Expected --count 0 to be rejected with a non-zero exit code\nstdout:\n%s\nstderr:\n%s", out.String(), errBuf.String())
			}
			if !strings.Contains(strings.ToLower(errBuf.String()), "count") {
				t.Errorf("Expected the error to mention --count, got %q", errBuf.String())
			}
			if sent.Load() != 0 {
				t.Errorf("Expected no requests for a rejected --count 0, got %d", sent.Load())
			}
		})
	}
}

func TestSinq_FiltersMatchingNothingFail(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "probe.sinq"), fmt.Appendf(nil, "GET %s/probe\n", srv.URL), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config.scenario"), []byte(`{"name": "probe", "tags": ["smoke"]}`), 0644)

	tests := []struct {
		name string
		args []string
	}{
		{name: "name typo", args: []string{"--no-spinner", "--name", "prboe", dir}},
		{name: "tag typo", args: []string{"--no-spinner", "--tag", "smoek", dir}},
		{name: "exclude everything", args: []string{"--no-spinner", "--no-tag", "smoke", dir}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent.Store(0)
			out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
			withCapturedIO(t, out, errBuf)

			if code := sinq(tt.args); code == 0 {
				t.Errorf("Expected a non-zero exit code when the filters leave nothing to run\nstdout:\n%s\nstderr:\n%s", out.String(), errBuf.String())
			}
			if !strings.Contains(errBuf.String(), "No scenarios to run") {
				t.Errorf("Expected the no-scenarios error, got %q", errBuf.String())
			}
			if sent.Load() != 0 {
				t.Errorf("Expected no requests, got %d", sent.Load())
			}
		})
	}
}

func TestSinq_JUnitFileIsRelativeToWorkingDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	ws := t.TempDir()
	scenarioDir := filepath.Join(ws, "tests", "integration", "auth")
	if err := os.MkdirAll(scenarioDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scenarioDir, "01_login.sinq"), fmt.Appendf(nil, "GET %s/login\n", srv.URL), 0644)

	files := func(t *testing.T, path string) []string {
		t.Helper()
		out := filepath.Join(t.TempDir(), "report.xml")
		withCapturedIO(t, &bytes.Buffer{}, &bytes.Buffer{})
		if code := sinq([]string{"--no-spinner", "-C", "never", "-f", "junit", "-o", out, path}); code != 0 {
			t.Fatalf("Expected exit code 0 for %q, got %d", path, code)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("Expected the report file, got %v", err)
		}
		var report struct {
			Suites []struct {
				Cases []struct {
					File string `xml:"file,attr"`
				} `xml:"testcase"`
			} `xml:"testsuite"`
		}
		if err := xml.Unmarshal(data, &report); err != nil {
			t.Fatalf("Invalid JUnit XML: %v\n%s", err, data)
		}
		res := []string{}
		for _, s := range report.Suites {
			for _, c := range s.Cases {
				res = append(res, c.File)
			}
		}
		return res
	}

	fromRoot := filepath.Join("tests", "integration", "auth", "01_login.sinq")
	cases := []struct {
		name string
		cwd  string
		path string
		want string
	}{
		{"nested folder", ws, filepath.Join("tests", "integration"), fromRoot},
		{"nested folder with dot slash", ws, "." + string(filepath.Separator) + filepath.Join("tests", "integration"), fromRoot},
		{"dot", ws, ".", fromRoot},
		{"absolute folder", ws, filepath.Join(ws, "tests", "integration"), fromRoot},
		{"single file", ws, fromRoot, fromRoot},
		{"single file by absolute path", ws, filepath.Join(ws, fromRoot), fromRoot},
		{"single file by bare name", scenarioDir, "01_login.sinq", "01_login.sinq"},
		{"parent of the working directory", filepath.Join(ws, "tests"), "..", filepath.Join("..", "tests", "integration", "auth", "01_login.sinq")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.cwd)
			if got := files(t, tc.path); len(got) != 1 || got[0] != tc.want {
				t.Errorf("Expected one testcase with file %q for path %q from %s, got %v", tc.want, tc.path, tc.cwd, got)
			}
		})
	}
}

func TestSinq_ExitCodesFollowTheWorstOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	passing := "GET " + srv.URL + "/ok\n"
	failing := "GET " + srv.URL + "/fail\n\n$ASSERT{ sinq.assert.fail(\"nope\") }\n"
	unreachable := "GET " + deadURL + "/gone\n"

	cases := []struct {
		name  string
		files map[string]string
		args  []string
		want  int
	}{
		{"everything passes", map[string]string{"a/01.sinq": passing}, nil, codeSuccess},
		{"an assertion fails", map[string]string{"a/01.sinq": passing, "b/01.sinq": failing}, nil, codeRunFailure},
		{"no response", map[string]string{"a/01.sinq": unreachable}, nil, codeRunError},
		{"an error outranks a failure", map[string]string{"a/01.sinq": failing, "b/01.sinq": unreachable}, nil, codeRunError},
		{"a failure stops a fail_fast scenario before its error", map[string]string{"a/01.sinq": failing, "a/02.sinq": unreachable}, nil, codeRunFailure},
		{"a failure and an error in one scenario", map[string]string{"a/config.scenario": `{"fail_fast": false}`, "a/01.sinq": failing, "a/02.sinq": unreachable}, nil, codeRunError},
		{"filters select nothing", map[string]string{"a/01.sinq": passing}, []string{"--name", "nomatch"}, codeSetupError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				_ = os.WriteFile(path, []byte(content), 0644)
			}
			errBuf := &bytes.Buffer{}
			withCapturedIO(t, &bytes.Buffer{}, errBuf)

			args := append([]string{"--no-spinner", "-C", "never"}, tc.args...)
			if got := sinq(append(args, dir)); got != tc.want {
				t.Errorf("Expected exit code %d, got %d\nstderr:\n%s", tc.want, got, errBuf)
			}
		})
	}
}

func TestSinq_InterruptedRunExits130(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT to the test process")
	}
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { _ = syscall.Kill(os.Getpid(), syscall.SIGINT) })
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		_ = os.MkdirAll(filepath.Join(dir, name), 0755)
		_ = os.WriteFile(filepath.Join(dir, name, "01.sinq"), []byte("GET "+srv.URL+"/"+name+"\n"), 0644)
	}
	errBuf, outBuf := &bytes.Buffer{}, &bytes.Buffer{}
	withCapturedIO(t, outBuf, errBuf)

	if got := sinq([]string{"--no-spinner", "-C", "never", "-w", "1", "-f", "junit", dir}); got != codeAborted {
		t.Errorf("Expected exit code %d after an interrupt, got %d\nstderr:\n%s\nstdout:\n%s", codeAborted, got, errBuf, outBuf)
	}
}

func TestSinq_UnparseablePathFailsTheRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()

	good, bad, worse := t.TempDir(), t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(good, "01.sinq"), []byte("GET "+srv.URL+"/ok\n"), 0644)
	_ = os.WriteFile(filepath.Join(bad, "broken_script.sinq"), []byte("GET "+srv.URL+"/bad\n\n$PRE{ unterminated\n"), 0644)
	_ = os.WriteFile(filepath.Join(worse, "01.sinq"), []byte("GET "+srv.URL+"/worse\n"), 0644)
	_ = os.WriteFile(filepath.Join(worse, "config.scenario"), []byte("{broken json"), 0644)

	cases := []struct {
		name string
		args []string
	}{
		{"good path first", []string{good, bad}},
		{"bad path first", []string{bad, good}},
		{"two broken paths", []string{good, bad, worse}},
		{"listing", []string{"--list", good, bad}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits.Store(0)
			errBuf := &bytes.Buffer{}
			withCapturedIO(t, &bytes.Buffer{}, errBuf)

			if got := sinq(append([]string{"--no-spinner", "-C", "never"}, tc.args...)); got != codeSetupError {
				t.Errorf("Expected exit code %d, got %d\nstderr:\n%s", codeSetupError, got, errBuf)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("Expected nothing to run with a broken path, the server got %d requests", n)
			}
			if !strings.HasPrefix(errBuf.String(), "Error: Failed to parse scenarios:\n  ") {
				t.Errorf("Expected the parse errors listed under one heading, got:\n%s", errBuf)
			}
			if !strings.Contains(errBuf.String(), "broken_script.sinq:4:1: ") {
				t.Errorf("Expected the parse error to name the broken file and spot, got:\n%s", errBuf)
			}
			if slices.Contains(tc.args, worse) && !strings.Contains(errBuf.String(), "config.scenario") {
				t.Errorf("Expected every path's parse error, the config.scenario one is missing:\n%s", errBuf)
			}
			if strings.Contains(errBuf.String(), "mistype filters") {
				t.Errorf("Expected no hint about filters for a parse error, got:\n%s", errBuf)
			}
		})
	}
}

func TestSinq_OverlappingPathsResolveAgainstTheirOwnRoot(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()

	ws := t.TempDir()
	inner := filepath.Join(ws, "tests", "auth", "inner")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(inner, "body.json"), []byte(`{"a":1}`), 0644)
	_ = os.WriteFile(filepath.Join(inner, "01.sinq"), []byte("POST "+srv.URL+"/\n\n$PRE{ req.attach(\"body.json\") }\n"), 0644)

	for _, order := range [][]string{{"tests", filepath.Join("tests", "auth")}, {filepath.Join("tests", "auth"), "tests"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			t.Chdir(ws)
			hits.Store(0)
			outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
			withCapturedIO(t, outBuf, errBuf)

			if got := sinq(append([]string{"--no-spinner", "-C", "never", "-S", "all"}, order...)); got != codeSuccess {
				t.Errorf("Expected exit code %d, got %d\nreport:\n%s\nstderr:\n%s", codeSuccess, got, outBuf, errBuf)
			}
			if n := hits.Load(); n != 2 {
				t.Errorf("Expected the scenario to send its request once per root (2), got %d", n)
			}
		})
	}
}

func TestSinq_ReportWriteFailureFailsTheRun(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("needs /dev/full")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "01.sinq"), []byte("GET "+srv.URL+"/\n"), 0644)
	errBuf := &bytes.Buffer{}
	withCapturedIO(t, &bytes.Buffer{}, errBuf)

	if got := sinq([]string{"--no-spinner", "-C", "never", "-f", "junit", "-o", "/dev/full", dir}); got != codeSetupError {
		t.Errorf("Expected exit code %d when the report can't be written, got %d\nstderr:\n%s", codeSetupError, got, errBuf)
	}
	if !strings.Contains(errBuf.String(), "Failed to report results") {
		t.Errorf("Expected the write failure on stderr, got:\n%s", errBuf)
	}
}

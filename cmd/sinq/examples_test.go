// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_ExamplesDirectory(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		checkAuth := func() bool {
			if r.Header.Get("Authorization") != "Bearer sys-jwt-999" {
				w.WriteHeader(http.StatusUnauthorized)
				return false
			}
			return true
		}

		switch {
		case r.URL.Path == "/system/login" && r.Method == "POST":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"token": "sys-jwt-999"}`)

		case r.URL.Path == "/players" && r.Method == "POST":
			if !checkAuth() {
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id": 42, "name": "Scyth"}`)

		case r.URL.Path == "/dungeons/join" && r.Method == "POST":
			if !checkAuth() {
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status": "entered", "enemies": 5}`)

		case r.URL.Path == "/trade/buy" && r.Method == "POST":
			if !checkAuth() {
				return
			}
			// Sleep for 8 seconds. This will fail unless the leaf overrides the 5s root timeout.
			time.Sleep(8 * time.Second)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status": "purchased", "gold_remaining": 150}`)

		case strings.Contains(r.URL.Path, "/quests/evolution") && r.Method == "GET":
			mu.Lock()
			defer mu.Unlock()
			pollCount++
			if pollCount < 3 {
				fmt.Fprint(w, `{"status": "calculating_stats"}`)
			} else {
				fmt.Fprint(w, `{"status": "complete"}`)
			}

		case r.URL.Path == "/guilds/spellswords" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status": "exists"}`)

		case r.URL.Path == "/guilds" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)

		case r.URL.Path == "/guilds/spellswords/join" && r.Method == "POST":
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/admin/wipe_server" && r.Method == "POST":
			w.WriteHeader(http.StatusUnauthorized)

		case r.URL.Path == "/shop/premium/purchase" && r.Method == "POST":
			if r.Header.Get("Authorization") != "Bearer super-secret-omni-coin-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	examplesDir, err := filepath.Abs("../../examples")
	if err != nil || func() bool { _, err := os.Stat(examplesDir); return os.IsNotExist(err) }() {
		t.Fatalf("Could not find examples directory at %s", examplesDir)
	}

	configPath := filepath.Join(examplesDir, "config.scenario")
	configData := map[string]any{
		"env": map[string]string{
			"BASE_URL": srv.URL,
		},
		"timeout": "5s",
	}
	configBytes, _ := json.MarshalIndent(configData, "", "  ")

	err = os.WriteFile(configPath, configBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write config.scenario: %v", err)
	}
	defer os.Remove(configPath)

	secretsPath := filepath.Join(examplesDir, "secrets.json")
	secretsData := map[string]string{
		"OMNI_COIN_API_KEY": "super-secret-omni-coin-key",
	}
	secretsBytes, _ := json.MarshalIndent(secretsData, "", "  ")

	err = os.WriteFile(secretsPath, secretsBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write secrets.json: %v", err)
	}
	defer os.Remove(secretsPath)

	args := []string{
		"--workers", "6",
		"--format", "default",
		"--secrets", secretsPath,
		examplesDir,
	}

	exitCode := sinq(args)

	if exitCode != 0 {
		t.Fatalf("Example test failed. Expected sinq to exit with 0, got %d", exitCode)
	}
}

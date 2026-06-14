// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_ExamplesDirectory(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0

	uploadBytes := make([]byte, 1024)
	rand.Read(uploadBytes)

	downloadBytes := make([]byte, 1024)
	rand.Read(downloadBytes)

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
			time.Sleep(250 * time.Millisecond)
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

		case r.URL.Path == "/upload" && r.Method == "POST":
			isChunked := r.ContentLength == -1
			if slices.Contains(r.TransferEncoding, "chunked") {
				isChunked = true
			}

			if !isChunked {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			data, err := io.ReadAll(r.Body)
			if len(data) > 0 && err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if !bytes.Equal(data, uploadBytes) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/download" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			w.Write(downloadBytes)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	examplesDir, err := filepath.Abs("../../examples")
	if err != nil || func() bool { _, err := os.Stat(examplesDir); return os.IsNotExist(err) }() {
		t.Fatalf("Could not find examples directory at %s", examplesDir)
	}

	assetsDir := filepath.Join(examplesDir, "assets")
	err = os.MkdirAll(assetsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create assets directory: %v", err)
	}
	defer os.Remove(assetsDir)

	uploadFilePath := filepath.Join(assetsDir, "file")
	err = os.WriteFile(uploadFilePath, uploadBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write upload file: %v", err)
	}
	defer os.Remove(uploadFilePath)

	configPath := filepath.Join(examplesDir, "config.scenario")
	configData := map[string]any{
		"env": map[string]string{
			"BASE_URL": srv.URL,
		},
		"timeout": "50ms",
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

	receivedFilePath := filepath.Join(assetsDir, "received_file")
	receivedData, err := os.ReadFile(receivedFilePath)
	if err != nil {
		t.Fatalf("Failed to read received file: %v", err)
	}
	defer os.Remove(receivedFilePath)

	if !bytes.Equal(receivedData, downloadBytes) {
		t.Fatalf("Received file content does not match the download bytes")
	}
}

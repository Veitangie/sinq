// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

	"github.com/golang-jwt/jwt/v5"
)

func Test_ExamplesDirectory(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0
	cache := 0
	optOutCount := 0

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

		case r.URL.Path == "/checkout" && r.Method == "POST":
			var body struct {
				Role          string `json:"role"`
				PaymentMethod string `json:"payment_method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			auth := r.Header.Get("Authorization")
			if body.Role == "admin" && auth != "Bearer sys-jwt-999" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if body.Role == "guest" && auth != "Bearer guest-jwt-111" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if body.PaymentMethod == "crypto" {
				w.WriteHeader(http.StatusAccepted)
			} else {
				w.WriteHeader(http.StatusOK)
			}

		case r.URL.Path == "/heavy-computation/opt-in" && r.Method == "GET":
			mu.Lock()
			cache++
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/heavy-computation/opt-out" && r.Method == "GET":
			mu.Lock()
			optOutCount++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/advanced/crypto" && r.Method == "POST":
			sig := r.Header.Get("X-Signature")
			b64Sig := r.Header.Get("X-Base64-Signature")
			if sig == "" || b64Sig == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			body, _ := io.ReadAll(r.Body)
			mac := hmac.New(sha256.New, []byte("my-secret"))
			mac.Write(body)
			expectedSig := hex.EncodeToString(mac.Sum(nil))
			if sig != expectedSig {
				t.Logf("sig mismatch: got %s, expected %s, body: %q", sig, expectedSig, string(body))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			expectedB64Sig := base64.StdEncoding.EncodeToString([]byte(expectedSig))
			if b64Sig != expectedB64Sig {
				t.Logf("b64 mismatch: got %s, expected %s", b64Sig, expectedB64Sig)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/advanced/jwt" && r.Method == "POST":
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				return []byte("jwt-secret"), nil
			})
			if err != nil || !token.Valid {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status": "verified"}`)

		case r.URL.Path == "/advanced/time" && r.Method == "GET":
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/advanced/faker" && r.Method == "POST":
			var body struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				Company     string `json:"company"`
				LuckyNumber int    `json:"lucky_number"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			t.Logf("FAKER BODY RECEIVED: %#v", body)

			if body.ID != "2f6279af-b981-416f-b728-036314a9c57a" ||
				body.Email != "acceptable+spirit@icloud.com" ||
				body.Company != "Singapore Afraid, LLC" ||
				body.LuckyNumber != 37 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/unreachable":
			t.Errorf("The /unreachable endpoint was hit! sinq.finishScenario() failed to abort the sequence.")
			w.WriteHeader(http.StatusBadRequest)

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

	expectedFilePath := filepath.Join(assetsDir, "expected_file")
	err = os.WriteFile(expectedFilePath, downloadBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write expected file: %v", err)
	}
	defer os.Remove(expectedFilePath)

	configPath := filepath.Join(examplesDir, "config.scenario")
	configData := map[string]any{
		"env": map[string]string{
			"BASE_URL": srv.URL,
		},
		"req_timeout": "200ms",
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
		"CRYPTO_SECRET":     "my-secret",
		"JWT_SECRET":        "jwt-secret",
	}
	secretsBytes, _ := json.MarshalIndent(secretsData, "", "  ")

	err = os.WriteFile(secretsPath, secretsBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write secrets.json: %v", err)
	}
	defer os.Remove(secretsPath)

	args := []string{
		"--workers", "25",
		"--format", "std",
		"--color", "always",
		"--secrets-file", secretsPath,
		"--plugins", filepath.Join(examplesDir, "plugins"),
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

	if cache != 1 {
		t.Fatalf("Expected exactly 1 request to /heavy-computation/opt-in due to singleflight deduplication, got %d", cache)
	}

	if optOutCount != 10 {
		t.Fatalf("Expected 10 requests to /heavy-computation/opt-out because singleflight was not used, got %d", optOutCount)
	}
}

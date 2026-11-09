// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	slowStartCalls := 0
	retryPolls := 0

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
				Role        string `json:"role"`
				LastSeenIP  string `json:"last_seen_ip"`
				Online      *bool  `json:"online"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			t.Logf("FAKER BODY RECEIVED: %#v", body)

			if body.ID != "2f6279af-b981-416f-b728-036314a9c57a" ||
				body.Email != "acceptable+spirit@icloud.com" ||
				body.Company != "Singapore Afraid, LLC" ||
				body.LuckyNumber != 37 ||
				!slices.Contains([]string{"warrior", "mage", "rogue"}, body.Role) ||
				net.ParseIP(body.LastSeenIP).To4() == nil ||
				body.Online == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/advanced/json/loadout" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"class": "Spellsword", "items": [{"id": 1, "rarity": "common"}, {"id": 2, "rarity": "rare"}], "stats": {}, "meta": "{\"version\": 3}"}`)

		case r.URL.Path == "/advanced/json/loadout" && r.Method == "PUT":
			var body struct {
				Loadout struct {
					Class string          `json:"class"`
					Items json.RawMessage `json:"items"`
					Stats json.RawMessage `json:"stats"`
				} `json:"loadout"`
				Legendary json.RawMessage `json:"legendary"`
				Skills    json.RawMessage `json:"skills"`
				Pet       json.RawMessage `json:"pet"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("loadout body is not valid JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			got := map[string]string{
				"loadout.class": body.Loadout.Class,
				"loadout.items": string(body.Loadout.Items),
				"loadout.stats": string(body.Loadout.Stats),
				"legendary":     string(body.Legendary),
				"skills":        string(body.Skills),
				"pet":           string(body.Pet),
			}
			want := map[string]string{
				"loadout.class": "Spellsword",
				"loadout.items": `[]`,
				"loadout.stats": `{}`,
				"legendary":     `[]`,
				"skills":        `["parry","riposte"]`,
				"pet":           `null`,
			}
			for field, w2 := range want {
				if got[field] != w2 {
					t.Logf("loadout field %s: got %s, want %s", field, got[field], w2)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/advanced/regex/latest" && r.Method == "GET":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Location", "/orders/1337")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "warn E42\nerror E77\ninfo boot\nerror E91\n")

		case r.URL.Path == "/advanced/regex/orders/1337" && r.Method == "POST":
			var body struct {
				Errors []string `json:"errors"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("regex report is not valid JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if want := []string{"E77", "E91"}; !slices.Equal(body.Errors, want) {
				t.Logf("regex report errors: got %q, want %q", body.Errors, want)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/advanced/encoding" && r.Method == "POST":
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			name, motto := r.PostForm.Get("name"), strings.TrimSpace(r.PostForm.Get("motto"))
			hexed, _ := hex.DecodeString(r.Header.Get("X-Hex"))
			b64url, _ := base64.URLEncoding.DecodeString(r.Header.Get("X-Base64-Url"))
			if name != "Scyth & co" || motto != "50% luck, 50/50 skill" || string(hexed) != "sinq?>>" || string(b64url) != "sinq?>>" {
				t.Logf("encoding: name=%q motto=%q hex=%q base64url=%q", name, motto, hexed, b64url)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"base64":    base64.StdEncoding.EncodeToString([]byte("sinq?>>")),
				"base64url": base64.URLEncoding.EncodeToString([]byte("sinq?>>")),
				"hex":       hex.EncodeToString([]byte("sinq?>>")),
				"query":     url.QueryEscape("a+b c"),
			})

		case r.URL.Path == "/advanced/crypto/blob" && r.Method == "GET":
			blob := []byte("Scyth's save file, slot 3\n")
			md5Sum, sha1Sum := md5.Sum(blob), sha1.Sum(blob)
			sha256Sum, sha512Sum := sha256.Sum256(blob), sha512.Sum512(blob)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(md5Sum[:]))
			w.Header().Set("X-Checksum-Sha1", hex.EncodeToString(sha1Sum[:]))
			w.Header().Set("X-Checksum-Sha256", hex.EncodeToString(sha256Sum[:]))
			w.Header().Set("X-Checksum-Sha512", base64.URLEncoding.EncodeToString(sha512Sum[:]))
			_, _ = w.Write(blob)

		case r.URL.Path == "/advanced/json/archive" && r.Method == "POST":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Version int    `json:"version"`
				Class   string `json:"class"`
			}
			if err := json.Unmarshal(raw, &body); err != nil || body.Version != 3 || body.Class != "Spellsword" || !strings.Contains(string(raw), "\n  \"") {
				t.Logf("archive body: %q", raw)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/upload/multipart" && r.Method == "POST":
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || params["boundary"] != "sinq-example-boundary" {
				t.Logf("multipart content type: %q", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Logf("multipart body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("image")
			if err != nil {
				t.Logf("multipart image part: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer file.Close()
			got, _ := io.ReadAll(file)
			if r.FormValue("caption") != "Scyth, level 40" || header.Filename != "portrait.bin" || !bytes.Equal(got, uploadBytes) {
				t.Logf("multipart: caption=%q filename=%q image matches=%v", r.FormValue("caption"), header.Filename, bytes.Equal(got, uploadBytes))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/retry/slow-start" && r.Method == "GET":
			mu.Lock()
			slowStartCalls++
			calls := slowStartCalls
			mu.Unlock()
			if calls == 1 {
				time.Sleep(150 * time.Millisecond)
			}
			fmt.Fprint(w, `{"status": "up"}`)

		case r.URL.Path == "/retry/poll" && r.Method == "GET":
			mu.Lock()
			retryPolls++
			polls := retryPolls
			mu.Unlock()
			if polls < 3 {
				fmt.Fprint(w, `{"status": "pending"}`)
				return
			}
			fmt.Fprint(w, `{"status": "done"}`)

		case r.URL.Path == "/responses/text" && r.Method == "GET":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "pong\n")

		case r.URL.Path == "/responses/large" && r.Method == "GET":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))

		case strings.HasPrefix(r.URL.Path, "/responses/hop/") && r.Method == "GET":
			hops, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/responses/hop/"))
			if hops == 0 {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, "landed")
				return
			}
			http.Redirect(w, r, "/responses/hop/"+strconv.Itoa(hops-1), http.StatusFound)

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
	// Written by examples/file-io; removed even when the run fails part-way.
	receivedFilePath := filepath.Join(assetsDir, "received_file")
	defer os.Remove(receivedFilePath)

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

	out := &bytes.Buffer{}
	withCapturedIO(t, out, out)
	exitCode := sinq(args)

	if exitCode != 0 {
		t.Fatalf("Example test failed. Expected sinq to exit with 0, got %d:\n%s", exitCode, out)
	}

	receivedData, err := os.ReadFile(receivedFilePath)
	if err != nil {
		t.Fatalf("Failed to read received file: %v", err)
	}

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

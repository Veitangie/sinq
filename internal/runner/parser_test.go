// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRequestParser_Parse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		validate    func(*testing.T, http.Request)
		errContains string
	}{
		{
			name: "Standard GET with Headers and Body",
			input: "GET https://api.example.com/v1/users HTTP/1.1\n" +
				"Authorization: Bearer 12345\n" +
				"Content-Type: application/json\n" +
				"\n" +
				`{"user_id": 1}`,
			validate: func(t *testing.T, r http.Request) {
				if r.Method != "GET" {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Scheme != "https" {
					t.Errorf("expected scheme https, got %s", r.URL.Scheme)
				}
				if r.URL.Host != "api.example.com" {
					t.Errorf("expected URL host api.example.com, got %s", r.URL.Host)
				}
				if r.Host != "api.example.com" {
					t.Errorf("expected req.Host api.example.com, got %s", r.Host)
				}
				if r.Header.Get("Authorization") != "Bearer 12345" {
					t.Errorf("wrong Auth header: %s", r.Header.Get("Authorization"))
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"user_id": 1}` {
					t.Errorf("wrong body: %s", string(body))
				}
				if r.ContentLength != 14 {
					t.Errorf("expected ContentLength 14, got %d", r.ContentLength)
				}
			},
		},
		{
			name: "Whitespace Leniency (Alignment)",
			input: "POST    https://auth.service.internal/api/login    HTTP/1.1\n" +
				"X-Custom-Header:    aligned-value\n" +
				"\n" +
				"body",
			validate: func(t *testing.T, r http.Request) {
				if r.Method != "POST" {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Host != "auth.service.internal" {
					t.Errorf("expected host auth.service.internal, got %s", r.URL.Host)
				}
				if r.Header.Get("X-Custom-Header") != "aligned-value" {
					t.Errorf("failed to trim whitespace around header value")
				}
			},
		},
		{
			name: "Host Header Promotion (Virtual Domain)",
			input: "GET https://10.0.0.1/foo HTTP/1.1\n" +
				"Host: virtual-host.local\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Host != "virtual-host.local" {
					t.Errorf("expected req.Host 'virtual-host.local', got %s", r.Host)
				}
				if r.URL.Host != "10.0.0.1" {
					t.Errorf("expected URL.Host '10.0.0.1', got %s", r.URL.Host)
				}
			},
		},
		{
			name: "Content-Length Override (Safety)",
			input: "POST https://data.io/ingest HTTP/1.1\n" +
				"Content-Length: 99999\n" +
				"\n" +
				"small",
			validate: func(t *testing.T, r http.Request) {
				if r.ContentLength != 5 {
					t.Errorf("parser should ignore user Content-Length, expected 5, got %d", r.ContentLength)
				}
			},
		},
		{
			name: "CRLF Line Endings Support",
			input: "DELETE https://resource.com/item HTTP/1.1\r\n" +
				"X-Test: true\r\n" +
				"\r\n" +
				"done",
			validate: func(t *testing.T, r http.Request) {
				if r.Method != "DELETE" {
					t.Errorf("failed to parse CRLF request line")
				}
				if r.Header.Get("X-Test") != "true" {
					t.Errorf("failed to parse CRLF header")
				}
			},
		},
		{
			name: "Default Headers Logic",
			input: "GET https://defaults.com/check HTTP/1.1\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("User-Agent") != "The Spanish Inquisition/1.0" {
					t.Errorf("missing default User-Agent")
				}
			},
		},
		{
			name: "User Overrides Defaults",
			input: "GET https://defaults.com/override HTTP/1.1\n" +
				"User-Agent: MyCustomBot\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("User-Agent") != "MyCustomBot" {
					t.Errorf("should respect user User-Agent")
				}
			},
		},
		{
			name: "Indented Headers (Treated as new header)",
			input: "GET https://fail.com/ HTTP/1.1\n" +
				"Header: value\n" +
				"  Folded: oops\n" +
				"\n",
			wantErr: false,
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("Folded") != "oops" {
					t.Errorf("expected indented line to be parsed as header 'Folded', got empty")
				}
			},
		},
		{
			name: "Edge Case: Value containing colons",
			input: "POST https://api.com/time HTTP/1.1\n" +
				"X-Time: 12:00:00\n" +
				"X-Json: {\"key\":\"value\"}\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("X-Time") != "12:00:00" {
					t.Errorf("failed to parse value with colons, got %s", r.Header.Get("X-Time"))
				}
				if r.Header.Get("X-Json") != `{"key":"value"}` {
					t.Errorf("failed to parse JSON in header, got %s", r.Header.Get("X-Json"))
				}
			},
		},
		{
			name: "Edge Case: Empty Header Value",
			input: "GET https://api.com/check HTTP/1.1\n" +
				"X-Empty-Header:\n" +
				"X-Empty-Header-With-Space: \n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if val := r.Header.Get("X-Empty-Header"); val != "" {
					t.Errorf("expected empty string, got %q", val)
				}
				if _, ok := r.Header["X-Empty-Header"]; !ok {
					t.Error("header key was skipped entirely")
				}
			},
		},
		{
			name: "Edge Case: Multiple/Duplicate Headers",
			input: "GET https://api.com/list HTTP/1.1\n" +
				"X-List: Item 1\n" +
				"X-List: Item 2\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				values := r.Header.Values("X-List")
				if len(values) != 2 {
					t.Errorf("expected 2 values, got %d", len(values))
				}
				if values[0] != "Item 1" || values[1] != "Item 2" {
					t.Errorf("wrong header order or values: %v", values)
				}
			},
		},
		{
			name: "URL: Query Parameters and Fragments",
			input: "GET https://api.com/search?q=hello&sort=asc#top HTTP/1.1\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.URL.RawQuery != "q=hello&sort=asc" {
					t.Errorf("query params lost, got %s", r.URL.RawQuery)
				}
				if r.URL.Fragment != "top" {
					t.Errorf("fragment lost, got %s", r.URL.Fragment)
				}
			},
		},
		{
			name: "Structure: No Headers (Body immediately after request line)",
			input: "POST https://api.com/echo HTTP/1.1\n" +
				"\n" +
				"Just Body",
			validate: func(t *testing.T, r http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != "Just Body" {
					t.Errorf("failed to parse body with no headers")
				}
			},
		},
		{
			name: "Structure: Missing HTTP Version (Defaults)",
			input: "GET https://api.com/simple\n" +
				"\n",
			wantErr: false,
		},
		{
			name: "Edge Case: EOF after Header (No newline)",
			input: "GET https://api.com/eof HTTP/1.1\n" +
				"X-Last: value",
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("X-Last") != "value" {
					t.Errorf("failed to parse last header before EOF")
				}
			},
		},
		{
			name: "Case Insensitivity",
			input: "GET https://api.com/ HTTP/1.1\n" +
				"content-type: application/json\n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("header canonicalization failed")
				}
			},
		},
		{
			name: "Panic on Empty Value",
			input: "GET https://api.com HTTP/1.1\n" +
				"Empty-Val:\n" +
				"\n",
			wantErr: false,
		},
		{
			name: "Trailing Whitespace",
			input: "GET https://api.com HTTP/1.1\n" +
				"Token: 123   \n" +
				"\n",
			wantErr: false,
		},
		{
			name: "Edge Case: Host Trailing Whitespace",
			input: "GET https://api.com HTTP/1.1\n" +
				"Host: custom-host.local   \n" +
				"\n",
			validate: func(t *testing.T, r http.Request) {
				if r.Host != "custom-host.local" {
					t.Errorf("expected trimmed host 'custom-host.local', got %q", r.Host)
				}
			},
		},
		// --- Failure Cases ---
		{
			name: "Fail: Abrupt EOF Mid-Header Key",
			input: "GET https://api.com HTTP/1.1\n" +
				"Head",
			wantErr:     true,
			errContains: "Unexpected end of file",
		},
		{
			name: "Fail: Header Missing Colon",
			input: "GET https://fail.com/ HTTP/1.1\n" +
				"NotAHeader\n" +
				"\n",
			wantErr:     true,
			errContains: "Malformed header",
		},
		{
			name: "Fail: Header Key Space",
			input: "GET https://fail.com/ HTTP/1.1\n" +
				"Space Key: value\n" +
				"\n",
			wantErr:     true,
			errContains: "Malformed header",
		},
		{
			name: "Space Before Colon (Alignment)",
			input: "GET https://api.com HTTP/1.1\n" +
				"Aligned : value\n" +
				"\n",
			wantErr:     true,
			errContains: "Malformed header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newParser([]byte(tt.input), context.Background())
			got, body, err := p.parse()
			got.Body = io.NopCloser(bytes.NewReader(body))

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func FuzzRequestParser_Heavy(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\nHost: a\r\n\r\n"))
	f.Add([]byte("POST / HTTP/1.1\n\nBody"))
	f.Add([]byte("GET\t/path\tHTTP/1.1\n\n"))
	f.Add([]byte("GET / HTTP/1.1\nHeader:"))
	f.Add([]byte("   GET / HTTP/1.1\n\n"))
	f.Add([]byte{0x00, 0x01, 0xFF})
	f.Add([]byte("GET / HTTP/1.1\nContent-Length: 999999999999999999\n\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := newParser(data, context.Background())
		if err == nil {
			_, _, _ = p.parse()
		}

		p2, err := newParser(data, context.Background())
		if err == nil {
			for p2.current < len(p2.source) {
				_, _ = p2.scanWord()
				_, _ = p2.scanLine()
				p2.skipWhitespace()

				if p2.current == 0 && len(data) > 0 {
					p2.current++
				}
			}
		}
	})
}

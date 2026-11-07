// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestValues_String(t *testing.T) {
	n := 5
	d := 3 * time.Second
	level := slog.LevelWarn
	format := "junit"
	size, err := ParseSize("10MiB")
	if err != nil {
		t.Fatalf("ParseSize failed: %v", err)
	}
	color := Always
	show := Failed
	regexes := []regexp.Regexp{*regexp.MustCompile("a.*"), *regexp.MustCompile("b")}

	tests := []struct {
		name  string
		unset pflag.Value
		set   pflag.Value
		want  string
	}{
		{"positive int", PositiveIntValue{}, PositiveIntValue{&n}, "5"},
		{"positive duration", PositiveDurationValue{}, PositiveDurationValue{&d}, "3s"},
		{"log level", LogLevelValue{}, LogLevelValue{&level}, "warn"},
		{"format", FormatValue{}, FormatValue{&format}, "junit"},
		{"data size", DataSizeValue{}, DataSizeValue{&size}, size.String()},
		{"color", WhenColorValue{}, WhenColorValue{&color}, "always"},
		{"show", WhatShowValue{}, WhatShowValue{&show}, "failed"},
		{"regex slice", RegexSliceValue{}, RegexSliceValue{&regexes}, "a.*, b"},
		{"env map", &EnvMapValue{}, &EnvMapValue{target: map[string]any{"k": "v"}}, "{...}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.unset.String(); got != "unset" {
				t.Errorf("Expected an unwired value to say %q, got %q", "unset", got)
			}
			if got := tt.set.String(); got != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestEnvMapValue_StringMasksSecrets(t *testing.T) {
	if got := (&EnvMapValue{target: map[string]any{}}).String(); got != "{}" {
		t.Errorf("Expected an empty map to show {}, got %q", got)
	}
	secret := &EnvMapValue{target: map[string]any{"token": "hunter2"}, isSecret: true}
	if got := secret.String(); got != "{ *** }" || strings.Contains(got, "hunter2") {
		t.Errorf("Expected secrets to be masked as { *** }, got %q", got)
	}
}

func TestLuaPathsValue_StringRoundTrips(t *testing.T) {
	var paths []string
	v := LuaPathsValue{&paths}
	input := strings.Join([]string{"plugins", "more"}, string(filepath.ListSeparator))
	if err := v.Set(input); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	var again []string
	if err := (LuaPathsValue{&again}).Set(v.String()); err != nil {
		t.Fatalf("Set of String() failed: %v", err)
	}
	if strings.Join(again, "|") != strings.Join(paths, "|") {
		t.Errorf("Expected String() to round-trip through Set: %v became %q and then %v", paths, v.String(), again)
	}
}

func TestPositiveIntValue_Set(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    int
	}{
		{"zero is rejected", "0", true, 0},
		{"negative is rejected", "-1", true, 0},
		{"positive is accepted", "1", false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := 0
			v := PositiveIntValue{&target}

			err := v.Set(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Set(%q) expected an error, got nil", tt.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("Set(%q) unexpected error: %v", tt.value, err)
			}
			if target != tt.want {
				t.Errorf("Set(%q) set target to %d, want %d", tt.value, target, tt.want)
			}
		})
	}
}

func TestPositiveDurationValue_Set(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    time.Duration
	}{
		{"zero is rejected", "0s", true, 0},
		{"negative is rejected", "-1s", true, 0},
		{"positive is accepted", "1s", false, 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target time.Duration
			v := PositiveDurationValue{&target}

			err := v.Set(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Set(%q) expected an error, got nil", tt.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("Set(%q) unexpected error: %v", tt.value, err)
			}
			if target != tt.want {
				t.Errorf("Set(%q) set target to %v, want %v", tt.value, target, tt.want)
			}
		})
	}
}

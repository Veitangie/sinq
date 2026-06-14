// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name       string
		flags      []string
		wantConfig Config
		wantErrs   int
	}{
		{
			name:       "Empty Flags (Sane Defaults)",
			flags:      []string{},
			wantConfig: SaneDefaults(),
			wantErrs:   0,
		},
		{
			name:  "Basic Worker Override (Short)",
			flags: []string{"-w", "5"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Workers = 5
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Basic Worker Override (Long)",
			flags: []string{"--workers", "20"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Workers = 20
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Boolean Flags Chaining",
			flags: []string{"-si"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Safe = true
				c.Insecure = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Color Options",
			flags: []string{"-c", "always"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Color = Always
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Double Dash Positional Terminator",
			flags: []string{"-s", "--", "-d", "--workers"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Safe = true
				c.Paths = []string{"-d", "--workers"}
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Chained Booleans Exhaustive",
			flags: []string{"-vVhl"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Version = true
				c.Verbose = true
				c.Help = true
				c.List = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:       "Color Never",
			flags:      []string{"-c", "never"},
			wantConfig: func() Config { c := SaneDefaults(); c.Reporter.Color = Never; return c }(),
			wantErrs:   0,
		},
		{
			name:       "Double Dash Stop Parsing",
			flags:      []string{"--"},
			wantConfig: SaneDefaults(),
			wantErrs:   0,
		},
		// --- Error Cases ---
		{
			name:       "Chained Boolean with Invalid Char",
			flags:      []string{"-svX"},
			wantConfig: func() Config { c := SaneDefaults(); c.Safe = true; c.Version = true; return c }(),
			wantErrs:   1,
		},
		{
			name:       "Invalid Color",
			flags:      []string{"-c", "magenta"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Missing Worker Value",
			flags:      []string{"-w"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Unknown Short Flag",
			flags:      []string{"-x"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Unknown Long Flag",
			flags:      []string{"--unknown"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Missing Value for Output",
			flags:      []string{"-o"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Invalid Worker Type",
			flags:      []string{"-w", "five"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Invalid Output Format",
			flags: []string{"--format", "yaml"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Format = "default"
				return c
			}(),
			wantErrs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Parse(tt.flags)
			gotConfig, gotErrs := p.Result()

			if len(gotErrs) != tt.wantErrs {
				t.Errorf("Expected %d errors, got %d", tt.wantErrs, len(gotErrs))
				for _, err := range gotErrs {
					t.Logf("Encountered error: %v", err)
				}
			}

			if !reflect.DeepEqual(gotConfig, tt.wantConfig) {
				t.Errorf("Config mismatch.\nGot:  %+v\nWant: %+v", gotConfig, tt.wantConfig)
			}
		})
	}
}

func TestParser_CommaFormatterLogic(t *testing.T) {
	p := NewParser()
	p.Parse([]string{"-f", "invalid_format"})
	_, errs := p.Result()

	if len(errs) == 0 {
		t.Fatal("Expected an error for invalid format, got none")
	}

	errMsg := errs[0].Error()
	if strings.Contains(errMsg, ", defaultjunit") || strings.Contains(errMsg, ", junitdefault") {
		t.Errorf("Comma formatter bug detected in error string: %s", errMsg)
	}
	if !strings.Contains(errMsg, "junit") || !strings.Contains(errMsg, "default") {
		t.Errorf("Error message missing known formats: %s", errMsg)
	}
}

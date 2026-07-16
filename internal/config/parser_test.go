// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"log/slog"
	"reflect"
	"regexp"
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
			flags: []string{"-li"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.List = true
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
			flags: []string{"-l", "--", "-d", "--workers"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.List = true
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
				c.Help = true
				c.List = true
				c.Reporter.Verbose = true
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
			name:       "Log Level Debug",
			flags:      []string{"-L", "Debug"},
			wantConfig: func() Config { c := SaneDefaults(); c.LogLevel = slog.LevelDebug; return c }(),
			wantErrs:   0,
		},
		{
			name:       "Double Dash Stop Parsing",
			flags:      []string{"--"},
			wantConfig: SaneDefaults(),
			wantErrs:   0,
		},
		{
			name:       "Chained Boolean with Invalid Char",
			flags:      []string{"-lvX"},
			wantConfig: func() Config { c := SaneDefaults(); c.List = true; c.Version = true; return c }(),
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
				c.Format = "std"
				return c
			}(),
			wantErrs: 1,
		},
		{
			name:       "Invalid Log Level",
			flags:      []string{"-L", "custom"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Show All (Long)",
			flags: []string{"--show", "all"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = All
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Show No-Skip (Short)",
			flags: []string{"-S", "no-skip"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = NoSkip
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Show Failures",
			flags: []string{"--show", "failures"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = Failures
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:       "Show Invalid",
			flags:      []string{"--show", "invalid"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Show Missing Value",
			flags:      []string{"-s"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Tag Include (Short)",
			flags: []string{"-t", "api"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsInclude = []string{"api"}
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Tag Include Multiple (Long)",
			flags: []string{"--tag", "api", "--tag", "ui"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsInclude = []string{"api", "ui"}
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Name Include Regex",
			flags: []string{"--name", "^Test"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.NamesInclude = append(c.NamesInclude, *regexp.MustCompile("^Test"))
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:       "Name Include Regex Invalid",
			flags:      []string{"--name", "([invalid"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Tag Exclude",
			flags: []string{"--skip-tag", "slow"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsExclude = []string{"slow"}
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Name Exclude Regex",
			flags: []string{"--skip-name", "Fail$"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.NamesExclude = append(c.NamesExclude, *regexp.MustCompile("Fail$"))
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:       "Name Exclude Regex Invalid",
			flags:      []string{"--skip-name", "([invalid"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Dump On Failure",
			flags: []string{"--dump-on-failure"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.DumpOnFailure = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Secret Inline (Short)",
			flags: []string{"-s", "API_KEY=123", "-s", "TOKEN=abc"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Secret["API_KEY"] = "123"
				c.Treewalker.Secret["TOKEN"] = "abc"
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Secret Inline Invalid (No Equal)",
			flags: []string{"-s", "API_KEY123"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:  "Env Inline (Short)",
			flags: []string{"-e", "HOST=localhost", "-e", "PORT=8080"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["HOST"] = "localhost"
				c.Treewalker.Env["PORT"] = "8080"
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Env Inline (Long)",
			flags: []string{"--env", "HOST=localhost=80"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["HOST"] = "localhost=80"
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Secrets File (Long)",
			flags: []string{"--secrets-file", "path/to/secrets.json"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.SecretsFile = "path/to/secrets.json"
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Safe Long Flag",
			flags: []string{"--safe"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Safe = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Plugins Flag",
			flags: []string{"--plugins", "path/to/plugins;path/to/more"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.LuaPaths = []string{"path/to/plugins", "path/to/more"}
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Unrestricted Mode (Short)",
			flags: []string{"-u"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Unrestricted = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:  "Unrestricted Mode (Long)",
			flags: []string{"--unrestricted"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Unrestricted = true
				return c
			}(),
			wantErrs: 0,
		},
		{
			name:       "Env Missing Value (Short)",
			flags:      []string{"-e"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Env Missing Equals",
			flags:      []string{"-e", "MY_KEY"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Env Empty Key",
			flags:      []string{"-e", "=123"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Secret Missing Equals",
			flags:      []string{"-s", "MY_SECRET"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Secret Empty Key",
			flags:      []string{"-s", "=abc"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Log Level Missing Value",
			flags:      []string{"-L"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Color Missing Value",
			flags:      []string{"-c"},
			wantConfig: SaneDefaults(),
			wantErrs:   1,
		},
		{
			name:       "Unknown Boolean Chained",
			flags:      []string{"-viX"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Version = true
				c.Insecure = true
				return c
			}(),
			wantErrs:   1,
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
	if strings.Contains(errMsg, ", stdjunit") || strings.Contains(errMsg, ", junitstd") {
		t.Errorf("Comma formatter bug detected in error string: %s", errMsg)
	}
	if !strings.Contains(errMsg, "junit") || !strings.Contains(errMsg, "std") {
		t.Errorf("Error message missing known formats: %s", errMsg)
	}
}

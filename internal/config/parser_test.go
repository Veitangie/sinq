// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"io"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name       string
		flags      []string
		wantConfig Config
		wantErr    bool
	}{
		{
			name:       "Empty Flags (Sane Defaults)",
			flags:      []string{},
			wantConfig: SaneDefaults(),
			wantErr:    false,
		},
		{
			name:  "Basic Worker Override (Short)",
			flags: []string{"-w", "5"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Workers = 5
				return c
			}(),
			wantErr: false,
		},
		{
			name:  "Basic Worker Override (Long)",
			flags: []string{"--workers", "20"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Workers = 20
				return c
			}(),
			wantErr: false,
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
			wantErr: false,
		},
		{
			name:  "Color Options",
			flags: []string{"-c", "always"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Color = Always
				return c
			}(),
			wantErr: false,
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
			wantErr: false,
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
			wantErr: false,
		},
		{
			name:       "Completion",
			flags:      []string{"--completion"},
			wantConfig: func() Config { c := SaneDefaults(); c.Completion = true; return c }(),
			wantErr:    false,
		},
		{
			name:       "Print Flag",
			flags:      []string{"--print"},
			wantConfig: func() Config { c := SaneDefaults(); c.Print = true; return c }(),
			wantErr:    false,
		},
		{
			name:       "No Spinner Flag",
			flags:      []string{"--no-spinner"},
			wantConfig: func() Config { c := SaneDefaults(); c.NoSpinner = true; return c }(),
			wantErr:    false},
		{
			name:       "Color Never",
			flags:      []string{"-c", "never"},
			wantConfig: func() Config { c := SaneDefaults(); c.Reporter.Color = Never; return c }(),
			wantErr:    false},
		{
			name:       "Log Level Debug",
			flags:      []string{"-L", "Debug"},
			wantConfig: func() Config { c := SaneDefaults(); c.LogLevel = slog.LevelDebug; return c }(),
			wantErr:    false},
		{
			name:       "Double Dash Stop Parsing",
			flags:      []string{"--"},
			wantConfig: SaneDefaults(),
			wantErr:    false},
		{
			name:       "Chained Boolean with Invalid Char",
			flags:      []string{"-lvX"},
			wantConfig: func() Config { c := SaneDefaults(); c.List = true; c.Version = true; return c }(),
			wantErr:    true,
		},
		{
			name:       "Invalid Color",
			flags:      []string{"-c", "magenta"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Missing Worker Value",
			flags:      []string{"-w"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Unknown Short Flag",
			flags:      []string{"-x"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Unknown Long Flag",
			flags:      []string{"--unknown"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Missing Value for Output",
			flags:      []string{"-o"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Invalid Worker Type",
			flags:      []string{"-w", "five"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Invalid Output Format",
			flags: []string{"--format", "yaml"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Format = "std"
				return c
			}(),
			wantErr: true,
		},
		{
			name:       "Invalid Log Level",
			flags:      []string{"-L", "custom"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Show All (Long)",
			flags: []string{"--show", "all"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = All
				return c
			}(),
			wantErr: false},
		{
			name:  "Show No-Skip (Short)",
			flags: []string{"-S", "no-skip"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = NoSkip
				return c
			}(),
			wantErr: false},
		{
			name:  "Show Failures",
			flags: []string{"--show", "failed"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Show = Failed
				return c
			}(),
			wantErr: false},
		{
			name:       "Show Invalid",
			flags:      []string{"--show", "invalid"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Show Missing Value",
			flags:      []string{"-s"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Tag Include (Short)",
			flags: []string{"-t", "api"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsInclude = []string{"api"}
				return c
			}(),
			wantErr: false},
		{
			name:  "Tag Include Multiple (Long)",
			flags: []string{"--tag", "api", "--tag", "ui"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsInclude = []string{"api", "ui"}
				return c
			}(),
			wantErr: false},
		{
			name:  "Name Include Regex",
			flags: []string{"--name", "^Test"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.NamesInclude = append(c.NamesInclude, *regexp.MustCompile("^Test"))
				return c
			}(),
			wantErr: false},
		{
			name:       "Name Include Regex Invalid",
			flags:      []string{"--name", "([invalid"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Tag Exclude",
			flags: []string{"--no-tag", "slow"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.TagsExclude = []string{"slow"}
				return c
			}(),
			wantErr: false},
		{
			name:  "Name Exclude Regex",
			flags: []string{"--no-name", "Fail$"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.NamesExclude = append(c.NamesExclude, *regexp.MustCompile("Fail$"))
				return c
			}(),
			wantErr: false},
		{
			name:       "Name Exclude Regex Invalid",
			flags:      []string{"--no-name", "([invalid"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Dump On Failure",
			flags: []string{"--dump-on-failure"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.DumpOnFailure = true
				return c
			}(),
			wantErr: false},
		{
			name:  "Secret Inline (Short)",
			flags: []string{"-s", "API_KEY=123", "-s", "TOKEN=abc"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Secrets["API_KEY"] = float64(123)
				c.Treewalker.Secrets["TOKEN"] = "abc"
				return c
			}(),
			wantErr: false},
		{
			name:       "Secret Inline Invalid (No Equal)",
			flags:      []string{"-s", "API_KEY123"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Env Inline (Short)",
			flags: []string{"-e", "HOST=localhost", "-e", "PORT=8080"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["HOST"] = "localhost"
				c.Treewalker.Env["PORT"] = float64(8080)
				return c
			}(),
			wantErr: false},
		{
			name:  "Env Inline (Long)",
			flags: []string{"--env", "HOST=localhost=80"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["HOST"] = "localhost=80"
				return c
			}(),
			wantErr: false},
		{
			name:  "Env Inline Nested Dot",
			flags: []string{"-e", "one.two.three=four"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["one"] = map[string]any{
					"two": map[string]any{
						"three": "four",
					},
				}
				return c
			}(),
			wantErr: false},
		{
			name:  "Secret Inline Nested JSON",
			flags: []string{"-s", `one={"two":{"three":"four"}}`},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Secrets["one"] = map[string]any{
					"two": map[string]any{
						"three": "four",
					},
				}
				return c
			}(),
			wantErr: false},
		{
			name:  "Secrets File (Long)",
			flags: []string{"--secrets-file", "path/to/secrets.json"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.SecretsFile = "path/to/secrets.json"
				return c
			}(),
			wantErr: false},
		{
			name:  "Plugins Flag",
			flags: []string{"--plugins", "path/to/plugins" + string(os.PathListSeparator) + "path/to/more"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.LuaPaths = []string{"path/to/plugins", "path/to/more"}
				return c
			}(),
			wantErr: false},
		{
			name:  "Unrestricted Mode (Short)",
			flags: []string{"-u"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Unrestricted = true
				return c
			}(),
			wantErr: false},
		{
			name:  "Unrestricted Mode (Long)",
			flags: []string{"--unrestricted"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Unrestricted = true
				return c
			}(),
			wantErr: false},
		{
			name:  "Max Cache Size",
			flags: []string{"--max-cache-size", "10MB"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.MaxCacheSize = DataSize{ByteAmount: 10 * (1 << 20), Unit: MiByte}
				return c
			}(),
			wantErr: false},
		{
			name:       "Max Cache Size Invalid",
			flags:      []string{"--max-cache-size", "invalid"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Max Cache Size Missing Value",
			flags:      []string{"--max-cache-size"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:  "Cache Timeout",
			flags: []string{"--cache-timeout", "5s"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.CacheTimeout = 5 * time.Second
				return c
			}(),
			wantErr: false},
		{
			name:       "Cache Timeout Invalid",
			flags:      []string{"--cache-timeout", "invalid"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Cache Timeout Missing Value",
			flags:      []string{"--cache-timeout"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Cache Timeout Negative",
			flags:      []string{"--cache-timeout", "-5s"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Env Missing Value (Short)",
			flags:      []string{"-e"},
			wantConfig: SaneDefaults(),
			wantErr:    true,
		},
		{
			name:       "Env Missing Equals",
			flags:      []string{"-e", "MY_KEY"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:       "Env Empty Key",
			flags:      []string{"-e", "=123"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:       "Secret Missing Equals",
			flags:      []string{"-s", "MY_SECRET"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:       "Secret Empty Key",
			flags:      []string{"-s", "=abc"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:       "Log Level Missing Value",
			flags:      []string{"-L"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:       "Color Missing Value",
			flags:      []string{"-c"},
			wantConfig: SaneDefaults(),
			wantErr:    true},
		{
			name:  "Unknown Boolean Chained",
			flags: []string{"-viX"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Version = true
				c.Insecure = true
				return c
			}(),
			wantErr: true},
		{
			name:  "Env Inline Deep Merge JSON",
			flags: []string{"-e", `API={"HOST":"localhost"}`, "-e", `API={"PORT":8080}`},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Treewalker.Env["API"] = map[string]any{
					"HOST": "localhost",
					"PORT": float64(8080),
				}
				return c
			}(),
			wantErr: false},
		{
			name:  "POSIX: Glued Short Flag Value (-w100)",
			flags: []string{"-w100"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Workers = 100
				return c
			}(),
			wantErr: false},
		{
			name:  "POSIX: Long Flag with Equals (--dump-on-failure=true)",
			flags: []string{"--dump-on-failure=true"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.DumpOnFailure = true
				return c
			}(),
			wantErr: false},
		{
			name:  "POSIX: Long Flag with Equals (--format=junit)",
			flags: []string{"--format=junit"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Format = "junit"
				return c
			}(),
			wantErr: false},
		{
			name:  "POSIX: Chained Boolean Into Value (-VSall)",
			flags: []string{"-VSall"},
			wantConfig: func() Config {
				c := SaneDefaults()
				c.Reporter.Verbose = true
				c.Reporter.Show = All
				return c
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(io.Discard)
			gotErr := p.Parse(tt.flags)
			gotConfig := p.Result()

			if (gotErr != nil) != tt.wantErr {
				t.Errorf("Expected errors: %t, got %v", tt.wantErr, gotErr)
			}

			if !reflect.DeepEqual(gotConfig, tt.wantConfig) {
				t.Errorf("Config mismatch.\nGot:  %+v\nWant: %+v", gotConfig, tt.wantConfig)
			}
		})
	}
}

func TestValues_RegexSliceValue_String_Bug(t *testing.T) {
	regexes := []regexp.Regexp{*regexp.MustCompile("test")}
	v := RegexSliceValue{target: &regexes}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("String() panicked! Likely due to make([]string, len, 0) bug: %v", r)
		}
	}()

	_ = v.String()
}

func TestValues_FormatValue_Set_Bug(t *testing.T) {
	var target string
	v := FormatValue{target: &target}

	err := v.Set("sTd")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if target != "std" {
		t.Fatalf("Expected target to be 'std' but got '%s'. Formatting should be normalized to lowercase.", target)
	}
}

func TestParser_CommaFormatterLogic(t *testing.T) {
	p := NewParser(io.Discard)
	err := p.Parse([]string{"-f", "invalid_format"})

	if err == nil {
		t.Fatal("Expected an error for invalid format, got none")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, ", stdjunit") || strings.Contains(errMsg, ", junitstd") {
		t.Errorf("Comma formatter bug detected in error string: %s", errMsg)
	}
	if !strings.Contains(errMsg, "junit") || !strings.Contains(errMsg, "std") {
		t.Errorf("Error message missing known formats: %s", errMsg)
	}
}

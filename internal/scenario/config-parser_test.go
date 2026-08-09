// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAdditionalData(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		initial *ScenarioConfig
		want    *ScenarioConfig
		wantErr bool
	}{
		{
			name:    "Parse tags",
			json:    `{"tags": ["api", "slow"]}`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{}},
			want:    &ScenarioConfig{Tags: map[string]struct{}{"api": {}, "slow": {}}},
			wantErr: false,
		},
		{
			name:    "Merge tags with existing",
			json:    `{"tags": ["api"]}`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{"slow": {}}},
			want:    &ScenarioConfig{Tags: map[string]struct{}{"api": {}, "slow": {}}},
			wantErr: false,
		},
		{
			name:    "Empty tags array",
			json:    `{"tags": []}`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{"slow": {}}},
			want:    &ScenarioConfig{Tags: map[string]struct{}{"slow": {}}},
			wantErr: false,
		},
		{
			name:    "Parse env matrix and tags",
			json:    `{"tags": ["api"], "env_matrix": [{"dev": {"url": "localhost"}}, {"prod": {"url": "example.com"}}]}`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{}},
			want: &ScenarioConfig{
				Tags: map[string]struct{}{"api": {}},
				EnvMatrix: []map[string]map[string]any{
					{"dev": {"url": "localhost"}},
					{"prod": {"url": "example.com"}},
				},
			},
			wantErr: false,
		},
		{
			name:    "Invalid env matrix",
			json:    `{"env_matrix": [{"dev": "invalid_string"}]}`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{}},
			want:    &ScenarioConfig{Tags: map[string]struct{}{}},
			wantErr: true,
		},
		{
			name:    "Invalid JSON",
			json:    `{"tags": ["api"`,
			initial: &ScenarioConfig{Tags: map[string]struct{}{}},
			want:    &ScenarioConfig{Tags: map[string]struct{}{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseAdditionalData(tt.initial, []byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAdditionalData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(tt.initial, tt.want) {
				t.Errorf("parseAdditionalData() got = %v, want %v", tt.initial, tt.want)
			}
		})
	}
}

func TestParseConfig_EnvDeepMergeAcrossFiles(t *testing.T) {
	parentJSON := `{
		"env": {
			"BASE_URL": "https://api.local",
			"NESTED": {"A": 1, "B": 2}
		}
	}`
	childJSON := `{
		"env": {
			"NESTED": {"A": 99}
		}
	}`

	cfg := SaneDefaultConfig()
	if err := ParseConfig(&cfg, strings.NewReader(parentJSON)); err != nil {
		t.Fatalf("ParseConfig(parent) failed: %v", err)
	}
	if err := ParseConfig(&cfg, strings.NewReader(childJSON)); err != nil {
		t.Fatalf("ParseConfig(child) failed: %v", err)
	}

	if cfg.Env["BASE_URL"] != "https://api.local" {
		t.Errorf("Expected BASE_URL to be inherited from the parent, got: %v", cfg.Env["BASE_URL"])
	}

	nested, ok := cfg.Env["NESTED"].(map[string]any)
	if !ok {
		t.Fatalf("Expected NESTED to be a map, got: %T %v", cfg.Env["NESTED"], cfg.Env["NESTED"])
	}

	if nested["A"] != float64(99) {
		t.Errorf("Expected NESTED.A to be overridden by the child to 99, got: %v", nested["A"])
	}
	if nested["B"] != float64(2) {
		t.Errorf("Expected NESTED.B to be preserved from the parent (deep merge), got: %v", nested["B"])
	}
}

func TestParseConfig_WithTags(t *testing.T) {
	jsonContent := `{
		"name": "Test API",
		"tags": ["api", "fast"]
	}`

	cfg := SaneDefaultConfig()
	err := ParseConfig(&cfg, strings.NewReader(jsonContent))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if cfg.Name != "Test API" {
		t.Errorf("Expected Name to be 'Test API', got '%s'", cfg.Name)
	}

	_, foundApi := cfg.Tags["api"]
	_, foundFast := cfg.Tags["fast"]
	if len(cfg.Tags) != 2 || !foundApi || !foundFast {
		t.Errorf("Expected Tags to contain 'api' and 'fast', got: %v", cfg.Tags)
	}
}

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
			initial: &ScenarioConfig{Tags: map[string]bool{}},
			want:    &ScenarioConfig{Tags: map[string]bool{"api": true, "slow": true}},
			wantErr: false,
		},
		{
			name:    "Merge tags with existing",
			json:    `{"tags": ["api"]}`,
			initial: &ScenarioConfig{Tags: map[string]bool{"slow": true}},
			want:    &ScenarioConfig{Tags: map[string]bool{"api": true, "slow": true}},
			wantErr: false,
		},
		{
			name:    "Empty tags array",
			json:    `{"tags": []}`,
			initial: &ScenarioConfig{Tags: map[string]bool{"slow": true}},
			want:    &ScenarioConfig{Tags: map[string]bool{"slow": true}},
			wantErr: false,
		},
		{
			name:    "Parse env matrix and tags",
			json:    `{"tags": ["api"], "env_matrix": [{"dev": {"url": "localhost"}}, {"prod": {"url": "example.com"}}]}`,
			initial: &ScenarioConfig{Tags: map[string]bool{}},
			want: &ScenarioConfig{
				Tags: map[string]bool{"api": true},
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
			initial: &ScenarioConfig{Tags: map[string]bool{}},
			want:    &ScenarioConfig{Tags: map[string]bool{}},
			wantErr: true,
		},
		{
			name:    "Invalid JSON",
			json:    `{"tags": ["api"`,
			initial: &ScenarioConfig{Tags: map[string]bool{}},
			want:    &ScenarioConfig{Tags: map[string]bool{}},
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

	if len(cfg.Tags) != 2 || !cfg.Tags["api"] || !cfg.Tags["fast"] {
		t.Errorf("Expected Tags to contain 'api' and 'fast', got: %v", cfg.Tags)
	}
}

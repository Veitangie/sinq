// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"regexp"
	"testing"
)

func TestConfig_ShouldInclude(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		scenarioTags map[string]struct{}
		scenarioName string
		want         bool
	}{
		{
			name:         "No filters, should include",
			config:       Config{},
			scenarioTags: map[string]struct{}{"api": {}},
			scenarioName: "Basic API Test",
			want:         true,
		},
		{
			name: "Tag in TagsExclude",
			config: Config{
				TagsExclude: []string{"slow"},
			},
			scenarioTags: map[string]struct{}{"api": {}, "slow": {}},
			scenarioName: "Slow API Test",
			want:         false,
		},
		{
			name: "Tag in TagsInclude",
			config: Config{
				TagsInclude: []string{"api"},
			},
			scenarioTags: map[string]struct{}{"api": {}, "fast": {}},
			scenarioName: "Fast API Test",
			want:         true,
		},
		{
			name: "Name matches NamesInclude",
			config: Config{
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Basic")},
			},
			scenarioTags: map[string]struct{}{"api": {}},
			scenarioName: "Basic API Test",
			want:         true,
		},
		{
			name: "Name matches NamesExclude",
			config: Config{
				NamesExclude: []regexp.Regexp{*regexp.MustCompile("Fail$")},
			},
			scenarioTags: map[string]struct{}{"api": {}},
			scenarioName: "Test Will Fail",
			want:         false,
		},
		{
			name: "Exclude takes priority over include (tag)",
			config: Config{
				TagsInclude: []string{"api"},
				TagsExclude: []string{"broken"},
			},
			scenarioTags: map[string]struct{}{"api": {}, "broken": {}},
			scenarioName: "Broken API Test",
			want:         false,
		},
		{
			name: "Exclude takes priority over include (name)",
			config: Config{
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Basic")},
				NamesExclude: []regexp.Regexp{*regexp.MustCompile("Test$")},
			},
			scenarioTags: map[string]struct{}{},
			scenarioName: "Basic Test",
			want:         false,
		},
		{
			name: "Multiple includes, any match",
			config: Config{
				TagsInclude:  []string{"api", "ui"},
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Core")},
			},
			scenarioTags: map[string]struct{}{"ui": {}},
			scenarioName: "UI Login",
			want:         false,
		},
		{
			name: "Multiple includes, both match (AND)",
			config: Config{
				TagsInclude:  []string{"api", "ui"},
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Core")},
			},
			scenarioTags: map[string]struct{}{"ui": {}},
			scenarioName: "Core Login",
			want:         true,
		},
		{
			name: "Include set, but no match",
			config: Config{
				TagsInclude: []string{"backend"},
			},
			scenarioTags: map[string]struct{}{"frontend": {}},
			scenarioName: "Frontend Test",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.ShouldInclude(tt.scenarioTags, tt.scenarioName); got != tt.want {
				t.Errorf("Config.ShouldInclude() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataSizeString(t *testing.T) {
	ds := DataSize{ByteAmount: 1024, Unit: KiByte}
	if ds.String() != "1.000000KiB" {
		t.Errorf("expected 1.000000KiB, got %s", ds.String())
	}
}

func TestDataUnitString(t *testing.T) {
	tests := []struct {
		unit DataUnit
		want string
	}{
		{Byte, "B"},
		{KiByte, "KiB"},
		{MiByte, "MiB"},
		{GiByte, "GiB"},
		{DataUnit(0), ""},
	}

	for _, tt := range tests {
		if got := tt.unit.String(); got != tt.want {
			t.Errorf("DataUnit(%d).String() = %v, want %v", tt.unit, got, tt.want)
		}
	}
}

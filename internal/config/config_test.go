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
		scenarioTags map[string]bool
		scenarioName string
		want         bool
	}{
		{
			name:         "No filters, should include",
			config:       Config{},
			scenarioTags: map[string]bool{"api": true},
			scenarioName: "Basic API Test",
			want:         true,
		},
		{
			name: "Tag in TagsExclude",
			config: Config{
				TagsExclude: []string{"slow"},
			},
			scenarioTags: map[string]bool{"api": true, "slow": true},
			scenarioName: "Slow API Test",
			want:         false,
		},
		{
			name: "Tag in TagsInclude",
			config: Config{
				TagsInclude: []string{"api"},
			},
			scenarioTags: map[string]bool{"api": true, "fast": true},
			scenarioName: "Fast API Test",
			want:         true,
		},
		{
			name: "Name matches NamesInclude",
			config: Config{
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Basic")},
			},
			scenarioTags: map[string]bool{"api": true},
			scenarioName: "Basic API Test",
			want:         true,
		},
		{
			name: "Name matches NamesExclude",
			config: Config{
				NamesExclude: []regexp.Regexp{*regexp.MustCompile("Fail$")},
			},
			scenarioTags: map[string]bool{"api": true},
			scenarioName: "Test Will Fail",
			want:         false,
		},
		{
			name: "Exclude takes priority over include (tag)",
			config: Config{
				TagsInclude: []string{"api"},
				TagsExclude: []string{"broken"},
			},
			scenarioTags: map[string]bool{"api": true, "broken": true},
			scenarioName: "Broken API Test",
			want:         false,
		},
		{
			name: "Exclude takes priority over include (name)",
			config: Config{
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Basic")},
				NamesExclude: []regexp.Regexp{*regexp.MustCompile("Test$")},
			},
			scenarioTags: map[string]bool{},
			scenarioName: "Basic Test",
			want:         false,
		},
		{
			name: "Multiple includes, any match",
			config: Config{
				TagsInclude:  []string{"api", "ui"},
				NamesInclude: []regexp.Regexp{*regexp.MustCompile("^Core")},
			},
			scenarioTags: map[string]bool{"ui": true},
			scenarioName: "UI Login",
			want:         true,
		},
		{
			name: "Include set, but no match",
			config: Config{
				TagsInclude: []string{"backend"},
			},
			scenarioTags: map[string]bool{"frontend": true},
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

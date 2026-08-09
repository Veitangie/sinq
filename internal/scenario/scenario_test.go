// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"testing"
	"time"
)

func TestScenarioConfigString(t *testing.T) {
	sc := SaneDefaultConfig()
	sc.Name = "Test"
	str := sc.String()
	if str == "" {
		t.Errorf("Expected string representation")
	}
}

func TestDurationUnmarshalJSON(t *testing.T) {
	var d Duration
	err := d.UnmarshalJSON([]byte(`"5s"`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if d.Duration != 5*time.Second {
		t.Errorf("expected 5s, got %v", d.Duration)
	}
}

func TestScenarioBlueprintString(t *testing.T) {
	bp := ScenarioBlueprint{
		Config: &ScenarioConfig{},
		Requests: []*RequestBlueprint{
			{Filename: "test.sinq"},
		},
	}
	if bp.String() == "" {
		t.Errorf("Expected string representation")
	}
}

func TestRequestBlueprintString(t *testing.T) {
	bp := RequestBlueprint{
		Filename: "test.sinq",
	}
	if bp.String() == "" {
		t.Errorf("Expected string representation")
	}
}

func TestToken_IsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		start     int
		end       int
		wantEmpty bool
	}{
		{"Empty (diff 0)", 10, 10, true},
		{"Empty (diff 1)", 10, 11, true},
		{"Not Empty (diff 2)", 10, 12, false},
		{"Not Empty (diff 5)", 10, 15, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := Token{PayloadStart: tt.start, PayloadEnd: tt.end}
			if got := token.IsEmpty(); got != tt.wantEmpty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestToken_IsSpecialScript(t *testing.T) {
	tests := []struct {
		name        string
		tokenName   string
		wantSpecial bool
	}{
		{"PRE is special", "PRE", true},
		{"RETRY is special", "RETRY", true},
		{"ASSERT is special", "ASSERT", true},
		{"POST is special", "POST", true},
		{"Other script is not special", "OTHER", false},
		{"Empty name is not special", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := Token{Name: tt.tokenName}
			if got := token.IsSpecialScript(); got != tt.wantSpecial {
				t.Errorf("IsSpecialScript() = %v, want %v", got, tt.wantSpecial)
			}
		})
	}
}

func TestScenarioBlueprint_CountVariants(t *testing.T) {
	tests := []struct {
		name      string
		envMatrix []map[string]map[string]any
		want      int
	}{
		{
			name:      "Empty matrix",
			envMatrix: nil,
			want:      1,
		},
		{
			name: "Single element matrix",
			envMatrix: []map[string]map[string]any{
				{"a": {"b": 1}},
			},
			want: 1,
		},
		{
			name: "Matrix with two elements",
			envMatrix: []map[string]map[string]any{
				{
					"a": {"b": 1},
					"c": {"d": 2},
				},
			},
			want: 2,
		},
		{
			name: "Matrix with one empty element and one non-empty",
			envMatrix: []map[string]map[string]any{
				{},
				{"c": {"d": 2}},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := ScenarioBlueprint{
				Config: &ScenarioConfig{
					EnvMatrix: tt.envMatrix,
				},
			}
			if got := bp.CountVariants(); got != tt.want {
				t.Errorf("CountVairants() = %v, want %v", got, tt.want)
			}
		})
	}
}

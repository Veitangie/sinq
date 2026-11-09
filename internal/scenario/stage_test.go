// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"strings"
	"testing"
)

func parseStages(t *testing.T, raw string) []*StageBlueprint {
	t.Helper()
	stages, err := ParseStageBlueprints(strings.NewReader(raw), "stages.sinq", "")
	if err != nil {
		t.Fatalf("Failed to parse %q: %v", raw, err)
	}
	return stages
}

func TestStageBlueprint_ScriptOnlyStages(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		noRequest []bool
	}{
		{"hooks only", "$PRE{ a = 1 }\n$ASSERT{ b = 2 }\n$POST{ c = 3 }\n", []bool{true}},
		{"post only", "$POST{ c = 3 }\n", []bool{true}},
		{"request", "GET http://localhost/\n", []bool{false}},
		{"lone inline script is a request line", "${ env.URL }\n", []bool{false}},
		{"lone named script is a request line", "$URL{ return env.URL }\n", []bool{false}},
		{"script-only stage before a request", "### Prepare\n$PRE{ a = 1 }\n\n### Send\nGET http://localhost/\n", []bool{true, false}},
		{"script-only stage after a request", "### Send\nGET http://localhost/\n\n### Check\n$ASSERT{ b = 2 }\n", []bool{false, true}},
		{"two script-only stages", "### One\n$PRE{ a = 1 }\n### Two\n$PRE{ a = 2 }\n", []bool{true, true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages := parseStages(t, tt.raw)
			if len(stages) != len(tt.noRequest) {
				t.Fatalf("Expected %d stages, got %d", len(tt.noRequest), len(stages))
			}
			for i, want := range tt.noRequest {
				if got := stages[i].IsNoRequest(); got != want {
					t.Errorf("Stage %d: expected IsNoRequest() = %v, got %v", i+1, want, got)
				}
			}
		})
	}
}

func TestStageBlueprint_ScriptOnlyStagesKeepTheirNames(t *testing.T) {
	stages := parseStages(t, "### Prepare\n$PRE{ a = 1 }\n### Send\nGET http://localhost/\n")
	if len(stages) != 2 || stages[0].Name != "Prepare" || stages[1].Name != "Send" {
		names := make([]string, 0, len(stages))
		for _, s := range stages {
			names = append(names, s.Name)
		}
		t.Errorf("Expected stages Prepare and Send, got %q", names)
	}
}

func TestStageBlueprint_InvalidStages(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"retry without a request", "$PRE{ a = 1 }\n$RETRY{ return -1 }\n", "$RETRY needs a request to retry"},
		{"retry alone", "$RETRY{ return -1 }\n", "$RETRY needs a request to retry"},
		{"retry in a script-only stage before a request", "$RETRY{ return -1 }\n###\nGET http://localhost/\n", "$RETRY needs a request to retry"},
		{"trailing delimiter", "GET http://localhost/\n###\n", "Empty stage"},
		{"empty file", "", "Empty stage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStageBlueprints(strings.NewReader(tt.raw), "stages.sinq", "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

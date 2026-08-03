// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"testing"
)

func TestBuildAllPaths(t *testing.T) {
	tests := []struct {
		name          string
		input         []map[string]map[string]any
		wantTotal     int
		wantPathsKeys [][]string
	}{
		{
			name: "Standard 2x2 Matrix",
			input: []map[string]map[string]any{
				{"admin": {}, "guest": {}},
				{"success": {}, "fail": {}},
			},
			wantTotal: 4,
		},
		{
			name: "Velocity Edge Case (1x3 Matrix)",
			input: []map[string]map[string]any{
				{"a": {}, "b": {}, "c": {}},
			},
			wantTotal: 3,
		},
		{
			name:      "Empty Matrix",
			input:     []map[string]map[string]any{},
			wantTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPaths, gotTotal := buildAllPaths(tt.input)

			if gotTotal != tt.wantTotal {
				t.Errorf("buildAllPaths() total = %d, want %d", gotTotal, tt.wantTotal)
			}

			if len(gotPaths) != len(tt.input) {
				t.Fatalf("buildAllPaths() returned %d levels, want %d", len(gotPaths), len(tt.input))
			}

			for i, level := range gotPaths {
				if len(level) != len(tt.input[i]) {
					t.Errorf("Level %d has %d keys, want %d", i, len(level), len(tt.input[i]))
				}
			}
		})
	}
}

func TestTakePath(t *testing.T) {
	allPaths := [][]string{
		{"admin", "guest"},
		{"visa", "amex", "mc"},
	}

	tests := []struct {
		name      string
		pathIndex int
		want      []string
	}{
		{
			name:      "First path (0)",
			pathIndex: 0,
			want:      []string{"admin", "visa"},
		},
		{
			name:      "Last path (5)",
			pathIndex: 5,
			want:      []string{"guest", "mc"},
		},
		{
			name:      "Out of Bounds Wrap-Around (6 -> loops to 0)",
			pathIndex: 6,
			want:      []string{"admin", "visa"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := takePath(tt.pathIndex, allPaths)
			if len(got) != len(tt.want) {
				t.Fatalf("takePath() returned length %d, want %d", len(got), len(tt.want))
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("takePath()[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

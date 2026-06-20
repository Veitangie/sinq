// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"reflect"
	"testing"
)

func TestDeepCopy_Values(t *testing.T) {
	tests := []struct {
		name string
		src  map[string]any
		want map[string]any
	}{
		{
			name: "nil map",
			src:  nil,
			want: map[string]any{},
		},
		{
			name: "flat map",
			src:  map[string]any{"string": "val", "int": 1, "bool": true},
			want: map[string]any{"string": "val", "int": 1, "bool": true},
		},
		{
			name: "nested map",
			src:  map[string]any{"root": map[string]any{"child": "data"}},
			want: map[string]any{"root": map[string]any{"child": "data"}},
		},
		{
			name: "slice of mixed types",
			src:  map[string]any{"arr": []any{1, "two", map[string]any{"three": 3}}},
			want: map[string]any{"arr": []any{1, "two", map[string]any{"three": 3}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepCopy(tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deepCopy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeepCopy_Isolation(t *testing.T) {
	src := map[string]any{
		"nested": map[string]any{"key": "original_map"},
		"slice":  []any{map[string]any{"key": "original_slice_element"}},
	}

	dst := deepCopy(src)

	dst["nested"].(map[string]any)["key"] = "mutated_map"

	if src["nested"].(map[string]any)["key"] != "original_map" {
		t.Errorf("deepCopy leaked memory reference on nested map")
	}
}

func TestDeepMerge_Values(t *testing.T) {
	tests := []struct {
		name  string
		mut   map[string]any
		immut map[string]any
		want  map[string]any
	}{
		{
			name:  "both nil",
			mut:   nil,
			immut: nil,
			want:  nil,
		},
		{
			name:  "immut nil",
			mut:   map[string]any{"a": 1},
			immut: nil,
			want:  map[string]any{"a": 1},
		},
		{
			name:  "flat overwrite and addition",
			mut:   map[string]any{"keep": 1, "overwrite": "old"},
			immut: map[string]any{"overwrite": "new", "add": true},
			want:  map[string]any{"keep": 1, "overwrite": "new", "add": true},
		},
		{
			name:  "deep merge map into map",
			mut:   map[string]any{"root": map[string]any{"keep": 1, "overwrite": 1}},
			immut: map[string]any{"root": map[string]any{"overwrite": 2, "add": 3}},
			want:  map[string]any{"root": map[string]any{"keep": 1, "overwrite": 2, "add": 3}},
		},
		{
			name:  "type mismatch (scalar replaced by map)",
			mut:   map[string]any{"root": "scalar_value"},
			immut: map[string]any{"root": map[string]any{"new": "map"}},
			want:  map[string]any{"root": map[string]any{"new": "map"}},
		},
		{
			name:  "array full replacement",
			mut:   map[string]any{"arr": []any{1, 2}},
			immut: map[string]any{"arr": []any{3}},
			want:  map[string]any{"arr": []any{3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deepMerge(tt.mut, tt.immut)
			if !reflect.DeepEqual(tt.mut, tt.want) {
				t.Errorf("deepMerge() resulted in %v, want %v", tt.mut, tt.want)
			}
		})
	}
}

func TestDeepMerge_Isolation(t *testing.T) {
	mut := map[string]any{}
	immut := map[string]any{
		"new_map":   map[string]any{"key": "matrix_data"},
		"new_slice": []any{map[string]any{"key": "matrix_slice_data"}},
	}

	deepMerge(mut, immut)

	mut["new_map"].(map[string]any)["key"] = "polluted_map"

	if immut["new_map"].(map[string]any)["key"] != "matrix_data" {
		t.Errorf("deepMerge leaked map reference from immutable source")
	}
}

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

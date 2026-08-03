// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package envs

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
			got := DeepCopy(tt.src)
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

	dst := DeepCopy(src)

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
			DeepMerge(tt.mut, tt.immut)
			if !reflect.DeepEqual(tt.mut, tt.want) {
				t.Errorf("DeepMerge() resulted in %v, want %v", tt.mut, tt.want)
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

	DeepMerge(mut, immut)

	mut["new_map"].(map[string]any)["key"] = "polluted_map"

	if immut["new_map"].(map[string]any)["key"] != "matrix_data" {
		t.Errorf("DeepMerge leaked map reference from immutable source")
	}
}

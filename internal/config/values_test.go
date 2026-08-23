// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"testing"
	"time"
)

func TestPositiveIntValue_Set(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    int
	}{
		{"zero is rejected", "0", true, 0},
		{"negative is rejected", "-1", true, 0},
		{"positive is accepted", "1", false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := 0
			v := PositiveIntValue{&target}

			err := v.Set(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Set(%q) expected an error, got nil", tt.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("Set(%q) unexpected error: %v", tt.value, err)
			}
			if target != tt.want {
				t.Errorf("Set(%q) set target to %d, want %d", tt.value, target, tt.want)
			}
		})
	}
}

func TestPositiveDurationValue_Set(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    time.Duration
	}{
		{"zero is rejected", "0s", true, 0},
		{"negative is rejected", "-1s", true, 0},
		{"positive is accepted", "1s", false, 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target time.Duration
			v := PositiveDurationValue{&target}

			err := v.Set(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Set(%q) expected an error, got nil", tt.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("Set(%q) unexpected error: %v", tt.value, err)
			}
			if target != tt.want {
				t.Errorf("Set(%q) set target to %v, want %v", tt.value, target, tt.want)
			}
		})
	}
}

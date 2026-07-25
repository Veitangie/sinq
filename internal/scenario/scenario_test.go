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

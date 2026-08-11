// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"strings"
	"testing"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/sinq/internal/scenario"
)

func TestInfoPrinter_PrintVersion(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	err = printer.PrintVersion()
	if err != nil {
		t.Fatalf("PrintVersion failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "sinq v1.2.3 - ") {
		t.Errorf("Expected version string in output, got: %s", output)
	}
}

func TestInfoPrinter_PrintHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	err = printer.PrintHelp()
	if err != nil {
		t.Fatalf("PrintHelp failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, helpPrefix) || !strings.Contains(output, helpSuffix) {
		t.Errorf("Expected help prefix and suffix in output, got: %s", output)
	}
}

func TestInfoPrinter_PrintScenarios(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	scenarios := []runner.ScenarioBundle{
		{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config: &scenario.ScenarioConfig{
					Name:        "Scenario1",
					Description: "Desc1",
					Tags:        map[string]struct{}{"tag1": {}},
				},
				Requests: []*scenario.RequestBlueprint{
					{Filename: "req1.txt"},
					{Name: "req2"},
				},
			},
		},
		{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config: &scenario.ScenarioConfig{
					Name: "Scenario2",
					EnvMatrix: []map[string]map[string]any{
						{
							"a": {"b": 1},
							"c": {"d": 2},
						},
					},
				},
				Requests: []*scenario.RequestBlueprint{
					{Filename: "req3.txt"},
				},
			},
		},
	}

	cfg := config.Config{
		Reporter: config.ReporterConfig{
			Show: config.All,
		},
	}

	err = printer.PrintScenarios(scenarios, cfg, false)
	if err != nil {
		t.Fatalf("PrintScenarios failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Scenario1") {
		t.Errorf("Expected Scenario1 in output")
	}
	if !strings.Contains(output, "Desc1") {
		t.Errorf("Expected Desc1 in output")
	}
	if !strings.Contains(output, "tag1") {
		t.Errorf("Expected tag1 in output")
	}
	if !strings.Contains(output, "req1.txt") {
		t.Errorf("Expected req1.txt in output")
	}
	if !strings.Contains(output, "req2") {
		t.Errorf("Expected req2 in output")
	}
	if !strings.Contains(output, "Scenario2") {
		t.Errorf("Expected Scenario2 in output")
	}
	if !strings.Contains(output, "(2 matrix combinations)") {
		t.Errorf("Expected matrix combos in output")
	}
	if !strings.Contains(output, "req3.txt") {
		t.Errorf("Expected req3.txt in output")
	}
}

func TestNewInfoPrinter_NilWriter(t *testing.T) {
	_, err := NewInfoPrinter(nil, "v1.2.3")
	if err == nil {
		t.Fatal("Expected error when writer is nil, got nil")
	}
}

func TestInfoPrinter_SetWriter_RedirectsPrintVersion(t *testing.T) {
	original := &bytes.Buffer{}
	redirected := &bytes.Buffer{}

	printer, err := NewInfoPrinter(original, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.SetWriter(redirected); err != nil {
		t.Fatalf("SetWriter failed: %v", err)
	}

	if err := printer.PrintVersion(); err != nil {
		t.Fatalf("PrintVersion failed: %v", err)
	}

	if original.Len() != 0 {
		t.Errorf("Expected nothing written to the original (pre-SetWriter) writer, got: %q", original.String())
	}
	if !strings.Contains(redirected.String(), "sinq v1.2.3 - ") {
		t.Errorf("Expected version string in the redirected writer, got: %q", redirected.String())
	}
}

func TestInfoPrinter_SetWriter_RedirectsPrintScenarios(t *testing.T) {
	original := &bytes.Buffer{}
	redirected := &bytes.Buffer{}

	printer, err := NewInfoPrinter(original, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.SetWriter(redirected); err != nil {
		t.Fatalf("SetWriter failed: %v", err)
	}

	scenarios := []runner.ScenarioBundle{
		{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config:   &scenario.ScenarioConfig{Name: "Scenario1"},
				Requests: []*scenario.RequestBlueprint{{Filename: "req1.txt"}},
			},
		},
	}
	cfg := config.Config{Reporter: config.ReporterConfig{Show: config.All}}

	if err := printer.PrintScenarios(scenarios, cfg, false); err != nil {
		t.Fatalf("PrintScenarios failed: %v", err)
	}

	if original.Len() != 0 {
		t.Errorf("Expected nothing written to the original (pre-SetWriter) writer, got: %q", original.String())
	}
	if !strings.Contains(redirected.String(), "Scenario1") {
		t.Errorf("Expected scenario listing in the redirected writer, got: %q", redirected.String())
	}
}

func TestInfoPrinter_SetWriter_NilWriterIsRejectedAndLeavesPrinterUsable(t *testing.T) {
	original := &bytes.Buffer{}
	printer, err := NewInfoPrinter(original, "v1.2.3")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.SetWriter(nil); err == nil {
		t.Fatal("Expected an error when setting a nil writer, got nil")
	}

	if err := printer.PrintVersion(); err != nil {
		t.Fatalf("PrintVersion failed after a rejected SetWriter(nil): %v", err)
	}
	if !strings.Contains(original.String(), "sinq v1.2.3 - ") {
		t.Errorf("Expected the original writer to still work after a rejected SetWriter(nil), got: %q", original.String())
	}
}

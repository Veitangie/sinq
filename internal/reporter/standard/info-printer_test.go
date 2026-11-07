// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/sinq/internal/scenario"
)

type nthErrorWriter struct {
	failOn int
	n      int
	buf    bytes.Buffer
}

func (w *nthErrorWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n == w.failOn {
		return 0, errors.New("simulated error on write")
	}
	return w.buf.Write(p)
}

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
		{
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
			Config:   &scenario.ScenarioConfig{Name: "Scenario1"},
			Requests: []*scenario.RequestBlueprint{{Filename: "req1.txt"}},
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

func TestInfoPrinter_PrintVersion_FirstWriteErrorPropagates(t *testing.T) {
	w := &nthErrorWriter{failOn: 1}
	printer, err := NewInfoPrinter(w, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.PrintVersion(); err == nil {
		t.Fatal("expected PrintVersion to propagate an error from the first write")
	}
	if w.n != 1 {
		t.Errorf("expected PrintVersion to stop after the first write, but the writer was called %d times", w.n)
	}
}

func TestInfoPrinter_PrintVersion_SecondWriteErrorPropagates(t *testing.T) {
	w := &nthErrorWriter{failOn: 2}
	printer, err := NewInfoPrinter(w, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.PrintVersion(); err == nil {
		t.Fatal("expected PrintVersion to propagate an error from the second write")
	}
}

func TestInfoPrinter_PrintHelp_ThirdWriteErrorPropagates(t *testing.T) {
	w := &nthErrorWriter{failOn: 3}
	printer, err := NewInfoPrinter(w, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	if err := printer.PrintHelp(); err == nil {
		t.Fatal("expected PrintHelp to propagate an error from the third write")
	}
}

func TestInfoPrinter_PrintScenarios_NoSkipFiltersExcluded(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	scenarios := []runner.ScenarioBundle{
		{
			Config:   &scenario.ScenarioConfig{Name: "ExcludedScenario", Tags: map[string]struct{}{"other": {}}},
			Requests: []*scenario.RequestBlueprint{{Filename: "r.txt"}}},
	}
	cfg := config.Config{
		Reporter:    config.ReporterConfig{Show: config.NoSkip},
		TagsInclude: []string{"needed"},
	}

	if err := printer.PrintScenarios(scenarios, cfg, false); err != nil {
		t.Fatalf("PrintScenarios failed: %v", err)
	}

	if strings.Contains(buf.String(), "ExcludedScenario") {
		t.Errorf("expected ExcludedScenario to be filtered out under Show:NoSkip, got:\n%s", buf.String())
	}
}

func TestInfoPrinter_PrintScenarios_NonNoSkipIgnoresFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	scenarios := []runner.ScenarioBundle{
		{
			Config:   &scenario.ScenarioConfig{Name: "IncludedScenario", Tags: map[string]struct{}{"other": {}}},
			Requests: []*scenario.RequestBlueprint{{Filename: "r.txt"}}},
	}
	cfg := config.Config{
		Reporter:    config.ReporterConfig{Show: config.All},
		TagsInclude: []string{"needed"},
	}

	if err := printer.PrintScenarios(scenarios, cfg, false); err != nil {
		t.Fatalf("PrintScenarios failed: %v", err)
	}

	if !strings.Contains(buf.String(), "IncludedScenario") {
		t.Errorf("expected IncludedScenario to be listed when Show is not NoSkip, got:\n%s", buf.String())
	}
}

func TestInfoPrinter_PrintScenarios_MatrixBoundaryAndHighlights(t *testing.T) {
	buf := &bytes.Buffer{}
	printer, err := NewInfoPrinter(buf, "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to create InfoPrinter: %v", err)
	}

	scenarios := []runner.ScenarioBundle{
		{
			Config: &scenario.ScenarioConfig{Name: "SingleCombo"},
			Requests: []*scenario.RequestBlueprint{
				{Filename: "r1.txt"},
				{Filename: "r2.txt"},
				{Filename: "r3.txt"},
			},
		},
	}
	cfg := config.Config{Reporter: config.ReporterConfig{Show: config.All}}

	if err := printer.PrintScenarios(scenarios, cfg, false); err != nil {
		t.Fatalf("PrintScenarios failed: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "matrix combinations") {
		t.Errorf("expected no matrix-combinations note for a single-combination scenario, got:\n%s", out)
	}

	idxR1 := strings.Index(out, "┣━r1.txt")
	idxR2 := strings.Index(out, "┣━r2.txt")
	idxR3 := strings.Index(out, "┗━r3.txt")
	if idxR1 == -1 || idxR2 == -1 || idxR3 == -1 {
		t.Fatalf("expected r1/r2 (not last) to use the middle highlight and r3 (last) to use the end highlight, got:\n%s", out)
	}
	if idxR1 >= idxR2 || idxR2 >= idxR3 {
		t.Errorf("expected requests to be printed in order r1,r2,r3, got:\n%s", out)
	}
}

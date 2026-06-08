// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/treewalker"
)

func mockParseRequest(r io.Reader, filename string) (*scenario.RequestBlueprint, error) {
	b, _ := io.ReadAll(r)
	return &scenario.RequestBlueprint{
		Source:  b,
		Content: []scenario.Token{{Type: scenario.Text, Start: 0, End: len(b), PayloadStart: 0, PayloadEnd: len(b)}},
	}, nil
}

func mockParseConfig(target *scenario.ScenarioConfig, r io.Reader) error {
	return json.NewDecoder(r).Decode(target)
}

func TestTreewalker_InheritanceAndLeaves(t *testing.T) {
	// root/
	//  ├── setup.sinq          (Should be inherited)
	//  ├── config.scenario     (Global Timeout: 10s, Env: GLOBAL=1)
	//  ├── A/                  (Leaf 1)
	//  │    ├── test.sinq
	//  │    └── config.scenario (Timeout: 5s, Env: A_VAL=2)
	//  └── B/                  (Leaf 2)
	//       └── test.sinq      (Should inherit 10s timeout)

	memFS := fstest.MapFS{
		"root/setup.sinq":        {Data: []byte("SETUP")},
		"root/config.scenario":   {Data: []byte(`{"timeout": "10s", "env": {"GLOBAL": "1"}}`)},
		"root/A/test.sinq":       {Data: []byte("TEST_A")},
		"root/A/config.scenario": {Data: []byte(`{"timeout": "5s", "env": {"A_VAL": "2"}}`)},
		"root/B/test.sinq":       {Data: []byte("TEST_B")},
	}

	cfg := config.Config{
		Workers:    1,
		Treewalker: config.TreewalkerConfig{Strict: false},
	}

	walker, err := treewalker.NewTreewalker(cfg, *slog.Default(), mockParseRequest, mockParseConfig)
	if err != nil {
		t.Fatalf("Failed to create walker: %v", err)
	}

	blueprints, err := walker.ParseFiletree(context.Background(), memFS)
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}

	if len(blueprints) != 2 {
		t.Fatalf("Expected 2 scenarios, got %d", len(blueprints))
	}

	findScenario := func(signature string) *scenario.ScenarioBlueprint {
		for _, bp := range blueprints {
			if len(bp.Requests) == 0 {
				continue
			}
			lastReq := bp.Requests[len(bp.Requests)-1]
			if string(lastReq.ExtractPayload(lastReq.Content[0])) == signature {
				return &bp
			}
		}
		return nil
	}

	bpA := findScenario("TEST_A")
	if bpA == nil {
		t.Fatal("Scenario A (TEST_A) not found")
	}
	if len(bpA.Requests) != 2 {
		t.Errorf("Scenario A: Expected 2 requests (inherited setup), got %d", len(bpA.Requests))
	}
	if bpA.Config.Timeout.Duration != 5*time.Second {
		t.Errorf("Scenario A: Expected timeout 5s (override), got %v", bpA.Config.Timeout)
	}
	if bpA.Config.Env["GLOBAL"] != "1" || bpA.Config.Env["A_VAL"] != "2" {
		t.Errorf("Scenario A: Env merging failed. Got %v", bpA.Config.Env)
	}

	bpB := findScenario("TEST_B")
	if bpB == nil {
		t.Fatal("Scenario B (TEST_B) not found")
	}
	if len(bpB.Requests) != 2 {
		t.Errorf("Scenario B: Expected 2 requests, got %d", len(bpB.Requests))
	}
	if bpB.Config.Timeout.Duration != 10*time.Second {
		t.Errorf("Scenario B: Expected timeout 10s (inherited), got %v", bpB.Config.Timeout)
	}
	if _, hasA := bpB.Config.Env["A_VAL"]; hasA {
		t.Error("Scenario B: Poisoned! Found env var from Sibling A")
	}
}

func TestTreewalker_CachePoisoning_Concurrency(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"env": {"COMMON": "BASE"}}`)},

		"root/branch1/req.sinq":        {Data: []byte("REQ1")},
		"root/branch1/config.scenario": {Data: []byte(`{"env": {"BRANCH": "ONE"}}`)},

		"root/branch2/req.sinq":        {Data: []byte("REQ2")},
		"root/branch2/config.scenario": {Data: []byte(`{"env": {"BRANCH": "TWO"}}`)},
	}

	cfg := config.Config{
		Workers: 4,
	}

	for i := range 20 {
		walker, _ := treewalker.NewTreewalker(cfg, *slog.Default(), mockParseRequest, mockParseConfig)
		blueprints, err := walker.ParseFiletree(context.Background(), memFS)
		if err != nil {
			t.Fatalf("Run %d: Parse failed: %v", i, err)
		}

		for _, bp := range blueprints {
			val := bp.Config.Env["BRANCH"]
			common := bp.Config.Env["COMMON"]

			if common != "BASE" {
				t.Fatalf("Run %d: Common env lost or corrupted", i)
			}

			lastReq := bp.Requests[len(bp.Requests)-1]
			reqContent := string(lastReq.ExtractPayload(lastReq.Content[0]))
			if reqContent == "REQ1" && val != "ONE" {
				t.Fatalf("Run %d: Branch 1 corrupted! Expected ONE, got %v", i, val)
			}
			if reqContent == "REQ2" && val != "TWO" {
				t.Fatalf("Run %d: Branch 2 corrupted! Expected TWO, got %v", i, val)
			}
		}
	}
}

func TestTreewalker_Ordering(t *testing.T) {
	// root/
	//  ├── z_setup_B.sinq       (Parent: Should be 2nd)
	//  ├── a_setup_A.sinq       (Parent: Should be 1st)
	//  └── sub/                 (Leaf)
	//       ├── b_test_B.sinq   (Child: Should be 4th)
	//       └── a_test_A.sinq   (Child: Should be 3rd)

	memFS := fstest.MapFS{
		"root/z_setup_B.sinq": {Data: []byte("REQ_2_PARENT_B")},
		"root/a_setup_A.sinq": {Data: []byte("REQ_1_PARENT_A")},

		"root/sub/b_test_B.sinq": {Data: []byte("REQ_4_CHILD_B")},
		"root/sub/a_test_A.sinq": {Data: []byte("REQ_3_CHILD_A")},
	}

	cfg := config.Config{Workers: 1}
	walker, _ := treewalker.NewTreewalker(cfg, *slog.Default(), mockParseRequest, mockParseConfig)

	blueprints, err := walker.ParseFiletree(context.Background(), memFS)
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}

	if len(blueprints) != 1 {
		t.Fatalf("Expected 1 blueprint, got %d", len(blueprints))
	}

	requests := blueprints[0].Requests
	if len(requests) != 4 {
		t.Fatalf("Expected 4 requests, got %d", len(requests))
	}

	expectedOrder := []string{
		"REQ_1_PARENT_A",
		"REQ_2_PARENT_B",
		"REQ_3_CHILD_A",
		"REQ_4_CHILD_B",
	}

	for i, expected := range expectedOrder {
		got := string(requests[i].ExtractPayload(requests[i].Content[0]))
		if got != expected {
			t.Errorf("Request index %d mismatch.\nExpected: %s\nGot:      %s", i, expected, got)
		}
	}
}

func TestTreewalker_Cancellation(t *testing.T) {
	memFS := fstest.MapFS{}
	for i := range 1000 {
		memFS[fmt.Sprintf("dir_%d/file.sinq", i)] = &fstest.MapFile{Data: []byte("GET /")}
	}

	cfg := config.Config{Workers: 2}
	tw, _ := treewalker.NewTreewalker(cfg, *slog.Default(), mockParseRequest, mockParseConfig)

	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan struct{})
	go func() {
		_, _ = tw.ParseFiletree(ctx, memFS)
		close(doneCh)
	}()

	time.Sleep(1 * time.Millisecond)
	cancel()

	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
		t.Fatal("Treewalker failed to exit after context cancellation (Deadlock detected)")
	}
}

// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/treewalker"
)

func newBus(t *testing.T) *event.Bus {
	t.Helper()
	bus, err := event.NewBus(64, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Failed to construct event bus: %v", err)
	}
	bus.Start()
	t.Cleanup(bus.Close)
	return bus
}

func mockParseStages(r io.Reader, filename, rootName string) ([]*scenario.StageBlueprint, error) {
	b, _ := io.ReadAll(r)
	return []*scenario.StageBlueprint{{
		Source:  b,
		Content: []scenario.Token{{Type: scenario.Text, Start: 0, End: len(b), PayloadStart: 0, PayloadEnd: len(b)}},
	}}, nil
}

func mockParseConfig(target *scenario.ScenarioConfig, r io.Reader) error {
	return scenario.ParseConfig(target, r)
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
		"root/config.scenario":   {Data: []byte(`{"req_timeout": "10s", "env": {"GLOBAL": "1"}}`)},
		"root/A/test.sinq":       {Data: []byte("TEST_A")},
		"root/A/config.scenario": {Data: []byte(`{"req_timeout": "5s", "env": {"A_VAL": "2"}}`)},
		"root/B/test.sinq":       {Data: []byte("TEST_B")},
	}

	cfg := config.Config{
		Workers:    1,
		Treewalker: config.TreewalkerConfig{},
	}

	walker, err := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")
	if err != nil {
		t.Fatalf("Failed to create walker: %v", err)
	}

	blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}

	if len(blueprints) != 2 {
		t.Fatalf("Expected 2 scenarios, got %d", len(blueprints))
	}

	findScenario := func(signature string) *scenario.ScenarioBlueprint {
		for _, bp := range blueprints {
			if len(bp.Stages) == 0 {
				continue
			}
			lastReq := bp.Stages[len(bp.Stages)-1]
			if string(lastReq.ExtractPayload(lastReq.Content[0])) == signature {
				return &bp
			}
		}
		return nil
	}

	bpA := findScenario("TEST_A")
	if bpA == nil {
		t.Fatal("Scenario A (TEST_A) not found")
		return
	}
	if len(bpA.Stages) != 2 {
		t.Errorf("Scenario A: Expected 2 requests (inherited setup), got %d", len(bpA.Stages))
	}
	if bpA.Config.ReqTimeout.Duration != 5*time.Second {
		t.Errorf("Scenario A: Expected timeout 5s (override), got %v", bpA.Config.ReqTimeout)
	}
	if bpA.Config.Env["GLOBAL"] != "1" || bpA.Config.Env["A_VAL"] != "2" {
		t.Errorf("Scenario A: Env merging failed. Got %v", bpA.Config.Env)
	}

	bpB := findScenario("TEST_B")
	if bpB == nil {
		t.Fatal("Scenario B (TEST_B) not found")
		return
	}
	if len(bpB.Stages) != 2 {
		t.Errorf("Scenario B: Expected 2 requests, got %d", len(bpB.Stages))
	}
	if bpB.Config.ReqTimeout.Duration != 10*time.Second {
		t.Errorf("Scenario B: Expected timeout 10s (inherited), got %v", bpB.Config.ReqTimeout)
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
		walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")
		blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
		if err != nil {
			t.Fatalf("Run %d: Parse failed: %v", i, err)
		}

		for _, bp := range blueprints {
			val := bp.Config.Env["BRANCH"]
			common := bp.Config.Env["COMMON"]

			if common != "BASE" {
				t.Fatalf("Run %d: Common env lost or corrupted", i)
			}

			lastReq := bp.Stages[len(bp.Stages)-1]
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

func TestTreewalker_TagsCachePoisoning_Concurrency(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"tags": ["BASE"]}`)},

		"root/branch1/req.sinq":        {Data: []byte("REQ1")},
		"root/branch1/config.scenario": {Data: []byte(`{"tags": ["ONE"]}`)},

		"root/branch2/req.sinq":        {Data: []byte("REQ2")},
		"root/branch2/config.scenario": {Data: []byte(`{"tags": ["TWO"]}`)},
	}

	cfg := config.Config{
		Workers: 4,
	}

	for i := range 20 {
		walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, scenario.ParseConfig, "")
		blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
		if err != nil {
			t.Fatalf("Run %d: Parse failed: %v", i, err)
		}

		for _, bp := range blueprints {
			if _, ok := bp.Config.Tags["BASE"]; !ok {
				t.Fatalf("Run %d: Common tag lost or corrupted", i)
			}

			lastReq := bp.Stages[len(bp.Stages)-1]
			reqContent := string(lastReq.ExtractPayload(lastReq.Content[0]))
			if reqContent == "REQ1" {
				if _, ok := bp.Config.Tags["ONE"]; !ok {
					t.Fatalf("Run %d: Branch 1 corrupted! Expected tag ONE", i)
				}
				if _, ok := bp.Config.Tags["TWO"]; ok {
					t.Fatalf("Run %d: Branch 1 leaked from Branch 2! Found tag TWO", i)
				}
			}
			if reqContent == "REQ2" {
				if _, ok := bp.Config.Tags["TWO"]; !ok {
					t.Fatalf("Run %d: Branch 2 corrupted! Expected tag TWO", i)
				}
				if _, ok := bp.Config.Tags["ONE"]; ok {
					t.Fatalf("Run %d: Branch 2 leaked from Branch 1! Found tag ONE", i)
				}
			}
		}
	}
}

func TestTreewalker_EnvMatrixCachePoisoning_Concurrency(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"env_matrix": [{"base": {"x": 1}}, {"base2": {"x": 2}}, {"base3": {"x": 3}}, {"base4": {"x": 4}}, {"base5": {"x": 5}}]}`)},

		"root/branch1/req.sinq":        {Data: []byte("REQ1")},
		"root/branch1/config.scenario": {Data: []byte(`{"env_matrix": [{"br1": {"x": 2}}]}`)},

		"root/branch2/req.sinq":        {Data: []byte("REQ2")},
		"root/branch2/config.scenario": {Data: []byte(`{"env_matrix": [{"br2": {"x": 3}}]}`)},
	}

	cfg := config.Config{Workers: 4}

	for i := range 20 {
		walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, scenario.ParseConfig, "")
		blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
		if err != nil {
			t.Fatalf("Run %d: Parse failed: %v", i, err)
		}

		for _, bp := range blueprints {
			if len(bp.Config.EnvMatrix) < 6 {
				t.Fatalf("Run %d: Missing base or branch matrix", i)
			}
			if _, ok := bp.Config.EnvMatrix[0]["base"]; !ok {
				t.Fatalf("Run %d: Base matrix corrupted", i)
			}
			lastReq := bp.Stages[len(bp.Stages)-1]
			reqContent := string(lastReq.ExtractPayload(lastReq.Content[0]))
			t.Logf("Run %d, branch %s EnvMatrix[5]: %v", i, reqContent, bp.Config.EnvMatrix[5])
			if reqContent == "REQ1" {
				if _, ok := bp.Config.EnvMatrix[5]["br1"]; !ok {
					t.Fatalf("Run %d: Branch 1 corrupted! Expected matrix br1", i)
				}
				if _, ok := bp.Config.EnvMatrix[5]["br2"]; ok {
					t.Fatalf("Run %d: Branch 1 leaked from Branch 2! Found matrix br2", i)
				}
			}
			if reqContent == "REQ2" {
				if _, ok := bp.Config.EnvMatrix[5]["br2"]; !ok {
					t.Fatalf("Run %d: Branch 2 corrupted! Expected matrix br2", i)
				}
				if _, ok := bp.Config.EnvMatrix[5]["br1"]; ok {
					t.Fatalf("Run %d: Branch 2 leaked from Branch 1! Found matrix br1", i)
				}
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
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")

	blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}

	if len(blueprints) != 1 {
		t.Fatalf("Expected 1 blueprint, got %d", len(blueprints))
	}

	requests := blueprints[0].Stages
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

func TestTreewalker_NaturalSortNotLexicographic(t *testing.T) {
	memFS := fstest.MapFS{
		"root/file2.sinq":  {Data: []byte("REQ_FILE2")},
		"root/file10.sinq": {Data: []byte("REQ_FILE10")},
	}

	cfg := config.Config{Workers: 1}
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")

	blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}
	if len(blueprints) != 1 {
		t.Fatalf("Expected 1 blueprint, got %d", len(blueprints))
	}

	requests := blueprints[0].Stages
	if len(requests) != 2 {
		t.Fatalf("Expected 2 requests, got %d", len(requests))
	}

	got0 := string(requests[0].ExtractPayload(requests[0].Content[0]))
	got1 := string(requests[1].ExtractPayload(requests[1].Content[0]))
	if got0 != "REQ_FILE2" || got1 != "REQ_FILE10" {
		t.Errorf("Expected natural order [REQ_FILE2, REQ_FILE10] (not lexicographic), got [%s, %s]", got0, got1)
	}
}

func TestTreewalker_CancellationReturnsError(t *testing.T) {
	memFS := fstest.MapFS{
		"root/file.sinq": {Data: []byte("GET /")},
	}

	cfg := config.Config{Workers: 2}
	tw, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tw.ParseFiletree(ctx, memFS, "root")
	if err == nil {
		t.Fatal("Expected ParseFiletree to return an error for an already-canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error to wrap context.Canceled, got: %v", err)
	}
}

func TestTreewalker_ExplicitScenarioNameIsNotOverwritten(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"name": "MyExplicitName"}`)},
		"root/req.sinq":        {Data: []byte("GET /")},
	}

	cfg := config.Config{Workers: 1}
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, scenario.ParseConfig, "")

	blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}
	if len(blueprints) != 1 {
		t.Fatalf("Expected 1 blueprint, got %d", len(blueprints))
	}

	if blueprints[0].Config.Name != "MyExplicitName" {
		t.Errorf("Expected explicit scenario name to be preserved, got %q", blueprints[0].Config.Name)
	}
}

func TestTreewalker_TagsListPopulated(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"tags": ["alpha", "beta"]}`)},
		"root/req.sinq":        {Data: []byte("GET /")},
	}

	cfg := config.Config{Workers: 1}
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, scenario.ParseConfig, "")

	blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}
	if len(blueprints) != 1 {
		t.Fatalf("Expected 1 blueprint, got %d", len(blueprints))
	}

	tagsList := blueprints[0].Config.TagsList
	if len(tagsList) != 2 {
		t.Fatalf("Expected TagsList to contain 2 tags, got %v", tagsList)
	}
	found := map[string]bool{}
	for _, tag := range tagsList {
		found[tag] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("Expected TagsList to contain alpha and beta, got %v", tagsList)
	}
}

func TestTreewalker_Cancellation(t *testing.T) {
	memFS := fstest.MapFS{}
	for i := range 1000 {
		memFS[fmt.Sprintf("dir_%d/file.sinq", i)] = &fstest.MapFile{Data: []byte("GET /")}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var parsed atomic.Int32
	cancellingParse := func(r io.Reader, filename, rootName string) ([]*scenario.StageBlueprint, error) {
		if parsed.Add(1) == 10 {
			cancel()
		}
		return mockParseStages(r, filename, rootName)
	}

	cfg := config.Config{Workers: 2}
	tw, _ := treewalker.NewTreewalker(cfg, newBus(t), cancellingParse, mockParseConfig, "")

	doneCh := make(chan error, 1)
	go func() {
		_, err := tw.ParseFiletree(ctx, memFS, "root")
		doneCh <- err
	}()

	select {
	case err := <-doneCh:
		if err == nil {
			t.Error("Expected a walk cancelled midway to return an error")
		}
		if n := parsed.Load(); n >= 1000 {
			t.Errorf("Expected the cancelled walk to stop early, but it parsed all %d files", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Treewalker failed to exit after context cancellation (Deadlock detected)")
	}
}
func TestTreewalker_MalformedConfig_GracefulFailure(t *testing.T) {
	memFS := fstest.MapFS{
		"root/config.scenario": {Data: []byte(`{"req_timeout": "5s", }`)},
		"root/test.sinq":       {Data: []byte("GET /")},
	}

	cfg := config.Config{Workers: 1}
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")

	_, err := walker.ParseFiletree(context.Background(), memFS, "root")

	if err == nil {
		t.Fatal("Expected Treewalker to return an error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid character") && !strings.Contains(err.Error(), "JSON") {
		t.Errorf("Expected a JSON syntax error, got: %v", err)
	}
}

func TestTreewalker_Constructors(t *testing.T) {
	cfg := config.Config{}
	_, err := treewalker.NewTreewalker(cfg, newBus(t), nil, mockParseConfig, "")
	if err == nil {
		t.Error("Expected error for nil parseRequest function")
	}
	_, err = treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, nil, "")
	if err == nil {
		t.Error("Expected error for nil parseScenarioConfig function")
	}
	for _, workers := range []int{0, -1} {
		if _, err = treewalker.NewTreewalker(config.Config{Workers: workers}, newBus(t), mockParseStages, mockParseConfig, ""); err == nil {
			t.Errorf("Expected error for %d workers", workers)
		}
	}
}

func TestTreewalker_ParseSecrets(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Valid JSON", func(t *testing.T) {
		secFile := filepath.Join(tmpDir, "valid.json")
		_ = os.WriteFile(secFile, []byte(`{"TOKEN": "123"}`), 0644)
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{SecretsFile: secFile}}, newBus(t), mockParseStages, mockParseConfig, "")
		secrets, err := tw.ParseSecrets()
		if err != nil || secrets["TOKEN"] != "123" {
			t.Errorf("Failed to parse valid secrets: %v", err)
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		secFile := filepath.Join(tmpDir, "invalid.json")
		_ = os.WriteFile(secFile, []byte(`{"TOKEN": "123"`), 0644)
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{SecretsFile: secFile}}, newBus(t), mockParseStages, mockParseConfig, "")
		_, err := tw.ParseSecrets()
		if err == nil {
			t.Error("Expected error for malformed JSON secrets file")
		}
	})

	t.Run("Empty File", func(t *testing.T) {
		secFile := filepath.Join(tmpDir, "empty.json")
		_ = os.WriteFile(secFile, []byte{}, 0644)
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{SecretsFile: secFile}}, newBus(t), mockParseStages, mockParseConfig, "")
		secrets, err := tw.ParseSecrets()
		if err != nil {
			t.Errorf("Expected no error for an empty secrets file, got: %v", err)
		}
		if len(secrets) != 0 {
			t.Errorf("Expected empty secrets map for an empty file, got: %v", secrets)
		}
	})

	t.Run("Missing File", func(t *testing.T) {
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{SecretsFile: filepath.Join(tmpDir, "missing.json")}}, newBus(t), mockParseStages, mockParseConfig, "")
		_, err := tw.ParseSecrets()
		if err == nil {
			t.Error("Expected error for non-existent secrets file")
		}
	})

	t.Run("Inline Override File", func(t *testing.T) {
		secFile := filepath.Join(tmpDir, "override.json")
		_ = os.WriteFile(secFile, []byte(`{"TOKEN": "123", "KEEP": "456"}`), 0644)
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{
			SecretsFile: secFile,
			Secrets: map[string]any{
				"TOKEN": "789",
				"NEW":   "000",
			},
		}}, newBus(t), mockParseStages, mockParseConfig, "")

		secrets, err := tw.ParseSecrets()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if secrets["TOKEN"] != "789" || secrets["KEEP"] != "456" || secrets["NEW"] != "000" {
			t.Errorf("Failed to merge inline secrets: %v", secrets)
		}
	})

	t.Run("Inline Only", func(t *testing.T) {
		tw, _ := treewalker.NewTreewalker(config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{
			Secrets: map[string]any{
				"TOKEN": "789",
			},
		}}, newBus(t), mockParseStages, mockParseConfig, "")

		secrets, err := tw.ParseSecrets()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if secrets["TOKEN"] != "789" {
			t.Errorf("Failed to parse inline secrets: %v", secrets)
		}
	})
}

type errFS struct{}

func (errFS) Open(name string) (fs.File, error) {
	return nil, errors.New("simulated open error")
}
func (errFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, errors.New("simulated readdir error")
}

func TestTreewalker_FileSystemErrors(t *testing.T) {
	cfg := config.Config{Workers: 1, Treewalker: config.TreewalkerConfig{}}
	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")

	_, err := walker.ParseFiletree(context.Background(), errFS{}, "root")
	if err == nil {
		t.Error("Expected error when FS fails to read directory")
	}
}

func TestTreewalker_ParseSingleFile(t *testing.T) {
	memFS := fstest.MapFS{
		"test.sinq": {Data: []byte("GET /")},
		"bad.sinq":  {Data: []byte("###\nGET /")},
	}

	cfg := config.Config{Workers: 1}

	walker, _ := treewalker.NewTreewalker(cfg, newBus(t), mockParseStages, mockParseConfig, "")
	for _, path := range []string{"test.sinq", "examples/basic/test.sinq", "../basic/test.sinq"} {
		bp, err := walker.ParseSingleFile(memFS, path, ".")
		if err != nil {
			t.Fatalf("ParseSingleFile(%q) failed: %v", path, err)
		}
		if bp.Config.Name != "test.sinq" {
			t.Errorf("ParseSingleFile(%q): expected the scenario to be named 'test.sinq', got %s", path, bp.Config.Name)
		}
	}

	_, err := walker.ParseSingleFile(memFS, "nonexistent.sinq", ".")
	if err == nil {
		t.Fatalf("Expected ParseSingleFile to fail on missing file")
	}

	simulated := errors.New("simulated parse error")
	failWalker, _ := treewalker.NewTreewalker(cfg, newBus(t), func(r io.Reader, fn, rootName string) ([]*scenario.StageBlueprint, error) {
		return nil, simulated
	}, mockParseConfig, "")

	_, err = failWalker.ParseSingleFile(memFS, "test.sinq", "some/dir")
	if !errors.Is(err, simulated) {
		t.Fatalf("Expected ParseSingleFile to fail with the parse error, got: %v", err)
	}
	if want := filepath.Join("some/dir", "test.sinq") + ": simulated parse error"; err.Error() != want {
		t.Errorf("Expected the error to name its file, want %q, got %q", want, err.Error())
	}
}

func seqIdTree() fstest.MapFS {
	memFS := fstest.MapFS{"root/setup.sinq": {Data: []byte("SETUP")}}
	for i := range 30 {
		memFS[fmt.Sprintf("root/dir_%d/test.sinq", i)] = &fstest.MapFile{Data: []byte("GET /")}
	}
	return memFS
}

func TestTreewalker_SeqIdUniqueAcrossRoots(t *testing.T) {
	walker, _ := treewalker.NewTreewalker(config.Config{Workers: 8}, newBus(t), mockParseStages, mockParseConfig, "")
	memFS := seqIdTree()

	seen := map[int]string{}
	record := func(source string, bp scenario.ScenarioBlueprint) {
		if bp.SeqId == 0 {
			t.Errorf("%s got seq id 0, which is reserved for blueprints the walker didn't build", source)
		}
		if other, ok := seen[bp.SeqId]; ok {
			t.Errorf("Seq id %d assigned twice: %s and %s", bp.SeqId, other, source)
		}
		seen[bp.SeqId] = source
	}

	for pass := range 2 {
		blueprints, err := walker.ParseFiletree(context.Background(), memFS, "root")
		if err != nil {
			t.Fatalf("ParseFiletree failed: %v", err)
		}
		for _, bp := range blueprints {
			record(fmt.Sprintf("walk %d %s", pass, bp.Config.Name), bp)
		}

		single, err := walker.ParseSingleFile(fstest.MapFS{"single.sinq": {Data: []byte("GET /")}}, "single.sinq", ".")
		if err != nil {
			t.Fatalf("ParseSingleFile failed: %v", err)
		}
		record(fmt.Sprintf("single file after walk %d", pass), single)
	}

	if len(seen) != 2*30+2 {
		t.Errorf("Expected %d distinct seq ids, got %d", 2*30+2, len(seen))
	}
}

func TestTreewalker_DefaultScenarioNames(t *testing.T) {
	names := func(memFS fstest.MapFS) []string {
		t.Helper()
		walker, _ := treewalker.NewTreewalker(config.Config{Workers: 2}, newBus(t), mockParseStages, scenario.ParseConfig, "")
		bps, err := walker.ParseFiletree(context.Background(), memFS, "matrix")
		if err != nil {
			t.Fatalf("ParseFiletree failed: %v", err)
		}
		res := []string{}
		for _, bp := range bps {
			res = append(res, bp.Config.Name)
		}
		slices.Sort(res)
		return res
	}

	cases := []struct {
		name  string
		fs    fstest.MapFS
		names []string
	}{
		{
			"files at the root take the root's name",
			fstest.MapFS{"01_checkout.sinq": {Data: []byte("A")}, "config.scenario": {Data: []byte(`{}`)}},
			[]string{"matrix"},
		},
		{
			"nested leaves keep their relative path",
			fstest.MapFS{"shared.sinq": {Data: []byte("S")}, "A/a.sinq": {Data: []byte("A")}, "B/deep/b.sinq": {Data: []byte("B")}},
			[]string{"A", "B/deep"},
		},
		{
			"an explicit name wins at the root",
			fstest.MapFS{"a.sinq": {Data: []byte("A")}, "config.scenario": {Data: []byte(`{"name": "explicit"}`)}},
			[]string{"explicit"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := names(tc.fs); !slices.Equal(got, tc.names) {
				t.Errorf("Expected scenario names %v, got %v", tc.names, got)
			}
		})
	}
}

func TestTreewalker_CachedStagesStayRelativeToTheirRoot(t *testing.T) {
	tree := fstest.MapFS{"auth/inner/01.sinq": {Data: []byte("GET http://x/\n")}}
	walker, err := treewalker.NewTreewalker(config.Config{Workers: 1}, newBus(t), scenario.ParseStageBlueprints, mockParseConfig, "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := walker.ParseFiletree(context.Background(), tree, "tests")
	if err != nil {
		t.Fatalf("First walk failed: %v", err)
	}
	sub, _ := fs.Sub(tree, "auth")
	second, err := walker.ParseFiletree(context.Background(), sub, filepath.Join("tests", "auth"))
	if err != nil {
		t.Fatalf("Second walk failed: %v", err)
	}

	if len(second) != 1 || len(second[0].Stages) != 1 {
		t.Fatalf("Expected one scenario with one stage, got %+v", second)
	}
	st := second[0].Stages[0]
	if st.Filename != "inner/01.sinq" || st.Rootname != filepath.Join("tests", "auth") {
		t.Errorf("Expected the stage relative to its own root (inner/01.sinq under tests/auth), got %q under %q", st.Filename, st.Rootname)
	}
	if len(first) != 1 || len(first[0].Stages) != 1 {
		t.Fatalf("Expected one scenario with one stage from the first walk, got %+v", first)
	}
	if st := first[0].Stages[0]; st.Filename != "auth/inner/01.sinq" || st.Rootname != "tests" {
		t.Errorf("Expected the first walk's stage to stay auth/inner/01.sinq under tests, got %q under %q", st.Filename, st.Rootname)
	}
}

func TestTreewalker_SharedCacheEntriesAreReadOnly(t *testing.T) {
	tree := fstest.MapFS{"00_setup.sinq": {Data: []byte("GET http://x/setup\n")}}
	for i := range 16 {
		tree[fmt.Sprintf("leaf%02d/01.sinq", i)] = &fstest.MapFile{Data: []byte("GET http://x/leaf\n")}
	}
	walker, err := treewalker.NewTreewalker(config.Config{Workers: 8}, newBus(t), scenario.ParseStageBlueprints, mockParseConfig, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		res, err := walker.ParseFiletree(context.Background(), tree, "root")
		if err != nil {
			t.Fatalf("Walk failed: %v", err)
		}
		for _, sc := range res {
			if st := sc.Stages[0]; st.Filename != "00_setup.sinq" || st.Rootname != "root" {
				t.Errorf("Expected the shared setup stage as 00_setup.sinq under root, got %q under %q", st.Filename, st.Rootname)
			}
		}
	}
}

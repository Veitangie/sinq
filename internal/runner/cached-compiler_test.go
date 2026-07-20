// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"hash/maphash"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
)

func setupTestCompiler() cachedCompiler {
	return cachedCompiler{hasherSeed: maphash.MakeSeed()}
}

func TestCachedCompiler_CacheMiss_DifferentKeys(t *testing.T) {
	cc := setupTestCompiler()

	extract := func(scenario.Token) []byte { return []byte(`return 1`) }

	token1 := scenario.Token{Name: "SCRIPT_A", Start: 0}
	token2 := scenario.Token{Name: "SCRIPT_B", Start: 0}
	token3 := scenario.Token{Name: "SCRIPT_A", Start: 10}

	proto1, _ := cc.compileScript(token1, extract, "file.sinq")
	proto2, _ := cc.compileScript(token2, extract, "file.sinq")
	proto3, _ := cc.compileScript(token3, extract, "file.sinq")
	proto4, _ := cc.compileScript(token1, extract, "different_file.sinq")

	if proto1 == proto2 || proto1 == proto3 || proto1 == proto4 {
		t.Error("Compiler returned same pointer for different cache keys")
	}

	count := 0
	cc.cache.Range(func(key, value any) bool { count++; return true })
	if count != 4 {
		t.Errorf("Expected cache map size to be 4, got %d", count)
	}
}

func TestCachedCompiler_SyntaxError_NotCached(t *testing.T) {
	cc := setupTestCompiler()

	token := scenario.Token{Name: "FAIL", Line: 1}
	extract := func(scenario.Token) []byte { return []byte(`if then syntax error end`) }

	_, err := cc.compileScript(token, extract, "fail.sinq")
	if err == nil {
		t.Fatal("Expected syntax error, got nil")
	}

	count := 0
	cc.cache.Range(func(key, value any) bool { count++; return true })
	if count != 0 {
		t.Errorf("Expected cache to be empty after failure, but got %d entries", count)
	}
}

func TestCachedCompiler_OptimisticConcurrency(t *testing.T) {
	cc := setupTestCompiler()

	token := scenario.Token{
		Name:   "RACE_CONDITION",
		Line:   1,
		Offset: 0,
		Start:  0,
	}

	var compileCount atomic.Int32
	extract := func(scenario.Token) []byte {
		compileCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return []byte(`return "optimistic"`)
	}

	goroutineCount := 100
	results := make([]*lua.FunctionProto, goroutineCount)
	errors := make([]error, goroutineCount)

	var startGate sync.WaitGroup
	var wg sync.WaitGroup

	startGate.Add(1)
	for i := range goroutineCount {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()

			proto, err := cc.compileScript(token, extract, "race.sinq")
			results[idx] = proto
			errors[idx] = err
		}(i)
	}

	startGate.Done()
	wg.Wait()

	var masterProto *lua.FunctionProto

	for i := range goroutineCount {
		if errors[i] != nil {
			t.Fatalf("Goroutine %d failed unexpectedly: %v", i, errors[i])
		}

		if masterProto == nil {
			masterProto = results[i]
		} else if results[i] != masterProto {
			t.Fatalf("Data race failure! Goroutine %d received a different prototype pointer. Expected %p, got %p", i, masterProto, results[i])
		}
	}

	finalCompileCount := compileCount.Load()
	if finalCompileCount <= 1 {
		t.Logf("Warning: Optimistic race didn't trigger. Call count was %d", finalCompileCount)
	}

	count := 0
	cc.cache.Range(func(key, value any) bool { count++; return true })
	if count != 1 {
		t.Errorf("Cache corruption! Expected exactly 1 entry in map, found %d", count)
	}
}

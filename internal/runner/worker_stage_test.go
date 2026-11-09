// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"strings"
	"testing"

	"veitangie.dev/sinq/internal/event"
)

func TestWorker_ProcessStage_ScriptOnlyStageRunsItsHooks(t *testing.T) {
	raw := "### Prepare\n" +
		"$PRE{ STEPS = \"pre\" }\n" +
		"$ASSERT{ STEPS = STEPS .. \",assert\" }\n" +
		"$POST{ STEPS = STEPS .. \",post\" }\n" +
		"### Check\n" +
		"$ASSERT{ sinq.assert.isTrue(STEPS == \"pre,assert,post\", \"hooks ran as \" .. tostring(STEPS)) }\n"
	res, obs := runSource(t, raw)

	if len(res.StageResults) != 2 {
		t.Fatalf("Expected 2 stage results, got %d", len(res.StageResults))
	}
	for _, rr := range res.StageResults {
		if rr.Status != event.Success {
			t.Errorf("Stage %q: expected Success, got %v %q %q", rr.Name, rr.Status, rr.ErrorMessage, rr.FailedAssertions)
		}
		if rr.Attempts != 0 {
			t.Errorf("Stage %q: expected no attempts for a stage that sends nothing, got %d", rr.Name, rr.Attempts)
		}
	}
	if ex := executions(obs); len(ex) != 0 {
		t.Errorf("Expected no Execution events, got %d", len(ex))
	}
}

func TestWorker_ProcessStage_ScriptOnlyStageHasNoResponse(t *testing.T) {
	raw := "### Send\nGET http://localhost/ HTTP/1.1\n\n" +
		"### Check\n" +
		"$ASSERT{\n" +
		"  sinq.assert.isTrue(rawget(res, \"code\") == nil, \"res should hold nothing\")\n" +
		"  sinq.assert.isTrue(sinq.responses[2] == nil, \"a script-only stage should have no response\")\n" +
		"  sinq.assert.isTrue(sinq.responses[1] ~= nil, \"earlier responses should stay available\")\n" +
		"}\n"
	res, _ := runSource(t, raw)

	if rr := res.StageResults[1]; rr.Status != event.Success {
		t.Errorf("Expected Success, got %v %q %q", rr.Status, rr.ErrorMessage, rr.FailedAssertions)
	}
}

// Reading or writing res where there is no request must say so, instead of
// failing with a nil index somewhere inside sinq's own Lua.
func TestWorker_ProcessStage_ResRaisesWithoutARequest(t *testing.T) {
	for _, hook := range []string{
		`$ASSERT{ local code = res.code }`,
		`$ASSERT{ local err = res.error }`,
		`$POST{ res.note = "x" }`,
		`$ASSERT{ sinq.assert.code(200) }`,
		`$ASSERT{ res.json() }`,
	} {
		t.Run(hook, func(t *testing.T) {
			res, _ := runSource(t, hook+"\n")

			rr := res.StageResults[0]
			if rr.Status != event.Error || !strings.Contains(rr.ErrorMessage, "res is not available in a stage without a request") {
				t.Errorf("Expected an Error explaining that res is unavailable, got %v %q", rr.Status, rr.ErrorMessage)
			}
		})
	}
}

func TestWorker_ProcessStage_ResErrorPointsAtTheUsersLine(t *testing.T) {
	res, _ := runSource(t, "\n\n$ASSERT{\n  local ok = true\n  local code = res.code\n}\n")

	if msg := res.StageResults[0].ErrorMessage; !strings.Contains(msg, "events.sinq#ASSERT:5:") {
		t.Errorf("Expected the error to name the line that read res, got %q", msg)
	}
}

func TestWorker_ProcessStage_EachScriptOnlyStageGetsItsOwnRes(t *testing.T) {
	raw := "### One\n" +
		"$PRE{ FIRST_RES = res; setmetatable(res, nil) }\n" +
		"### Two\n" +
		"$ASSERT{ local ok = pcall(function() return res.code end); sinq.assert.isTrue(not ok, \"the second stage's res should still raise\") }\n"
	res, _ := runSource(t, raw)

	if rr := res.StageResults[1]; rr.Status != event.Success {
		t.Errorf("Expected a stage's metatable change not to leak into the next stage, got %v %q %q", rr.Status, rr.ErrorMessage, rr.FailedAssertions)
	}
}

func TestWorker_ProcessStage_ScriptOnlyAssertionFails(t *testing.T) {
	res, _ := runSource(t, "$ASSERT{ sinq.assert.fail(\"nope\") }\n")

	if rr := res.StageResults[0]; rr.Status != event.Failure || len(rr.FailedAssertions) != 1 {
		t.Errorf("Expected a Failure with one failed assertion, got %v %q", rr.Status, rr.FailedAssertions)
	}
}

func TestWorker_ProcessStage_ScriptOnlyStageCanBeSkipped(t *testing.T) {
	res, _ := runSource(t, "$PRE{ req.skip() }\n$ASSERT{ sinq.assert.fail(\"should not run\") }\n")

	if rr := res.StageResults[0]; rr.Status != event.Skipped {
		t.Errorf("Expected Skipped, got %v %q %q", rr.Status, rr.ErrorMessage, rr.FailedAssertions)
	}
}

// Only req.skip has a meaning without a request; the rest must raise an error
// instead of quietly doing nothing.
func TestWorker_ProcessStage_ReqFunctionsFailWithoutARequest(t *testing.T) {
	for _, call := range []string{
		`req.attach("file")`,
		`req.saveResponseTo("out")`,
		`req.cache(true)`,
		`req.multipart({ name = "a", data = "b" })`,
		`req.multipartBoundary("boundary")`,
	} {
		t.Run(call, func(t *testing.T) {
			res, _ := runSource(t, "$PRE{ "+call+" }\n")

			rr := res.StageResults[0]
			if rr.Status != event.Error || !strings.Contains(rr.ErrorMessage, "non-request stage") {
				t.Errorf("Expected an Error about a non-request stage, got %v %q", rr.Status, rr.ErrorMessage)
			}
		})
	}
}

func TestWorker_ProcessStage_SetNextStageLoopsThroughAScriptOnlyStage(t *testing.T) {
	raw := "### Count\n" +
		"$POST{\n" +
		"  RUNS = (RUNS or 0) + 1\n" +
		"  if RUNS < 3 then sinq.setNextStage(1) end\n" +
		"}\n" +
		"### Check\n" +
		"$ASSERT{ sinq.assert.isTrue(RUNS == 3, \"ran \" .. tostring(RUNS) .. \" times\") }\n"
	res, _ := runSource(t, raw)

	if rr := res.StageResults[1]; rr.Status != event.Success {
		t.Errorf("Expected the loop to run 3 times, got %v %q", rr.Status, rr.FailedAssertions)
	}
}

// JUnit's file attribute comes from here: a named stage must still say which
// file it lives in.
func TestWorker_ProcessStage_StageResultsCarryTheirFile(t *testing.T) {
	res, _ := runSource(t, "### Named\nGET http://localhost/ HTTP/1.1\n\n###\nGET http://localhost/ HTTP/1.1\n")

	if len(res.StageResults) != 2 {
		t.Fatalf("Expected 2 stage results, got %d", len(res.StageResults))
	}
	for _, rr := range res.StageResults {
		if rr.Filename != "events.sinq" {
			t.Errorf("Stage %q: expected the file events.sinq, got %q", rr.Name, rr.Filename)
		}
	}
	if res.StageResults[0].Name != "Named" {
		t.Errorf("Expected the first stage to keep its name, got %q", res.StageResults[0].Name)
	}
}

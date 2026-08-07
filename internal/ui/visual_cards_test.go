package ui

import (
	"bytes"
	"strings"
	"testing"

	"shellia/internal/core"
)

func TestCardsRendererStreamsBeforeCloseAndClosesOnce(t *testing.T) {
	var out bytes.Buffer
	r := &Renderer{impl: newCardsRenderer(&out, false)}
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("first")
	if !strings.Contains(out.String(), "first") {
		t.Fatal("output was buffered until close")
	}

	step.Close()
	beforeTurnClose := out.String()
	turn.Final("done")
	turn.Close()
	afterFirstClose := out.String()
	turn.Close()

	if len(afterFirstClose) <= len(beforeTurnClose) {
		t.Fatal("turn close did not emit its final border")
	}
	if strings.Count(afterFirstClose, "\n└") != 1 {
		t.Fatalf("outer bottom border count = %d, want 1:\n%s", strings.Count(afterFirstClose, "\n└"), afterFirstClose)
	}
	if out.String() != afterFirstClose {
		t.Fatal("second Close changed card output")
	}
}

func TestCardsRendererShowsFlushedPartialOutput(t *testing.T) {
	var out bytes.Buffer
	turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	writer := &prefixedWriter{box: step}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(out.String(), "partial") {
		t.Fatalf("partial line rendered before Flush():\n%s", out.String())
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !strings.Contains(out.String(), "partial") {
		t.Fatalf("Flush() did not render partial output:\n%s", out.String())
	}
	step.Close()
	turn.Close()
}

func TestCardsRendererKeepsConversationHierarchy(t *testing.T) {
	output := renderConversationFixture(t, newCardsRenderer, false)
	assertOrdered(
		t,
		output,
		"┌─ Tu",
		"you › quant d'espai queda al disc?",
		"└",
		"┌─ Shellia",
		"plan",
		"step 1/1",
		"system output",
		"419Gi available",
		"Queden 419Gi lliures al disc arrel (/).",
	)
	if strings.Count(output, "┌─ Tu") != 1 || strings.Count(output, "┌─ Shellia") != 1 {
		t.Fatalf("actor cards were split into multiple cards:\n%s", output)
	}
}

func TestCardsRendererNoColorKeepsBordersWithoutANSI(t *testing.T) {
	output := renderConversationFixture(t, newCardsRenderer, false)
	if strings.Contains(output, "\033[") {
		t.Fatalf("no-color cards emitted ANSI: %q", output)
	}
	for _, want := range []string{"┌─ Tu", "┌─ Shellia", "│ ", " │", "└"} {
		if !strings.Contains(output, want) {
			t.Fatalf("no-color cards lack %q:\n%s", want, output)
		}
	}
}

func TestCardsRendererClosesFinalOnlyAndEarlyTurns(t *testing.T) {
	t.Run("final without step", func(t *testing.T) {
		var out bytes.Buffer
		turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
		turn.Final("blocked")
		turn.Close()
		if !strings.Contains(out.String(), "blocked") || strings.Count(out.String(), "\n└") != 1 {
			t.Fatalf("final-only turn was not closed once:\n%s", out.String())
		}
	})

	t.Run("close before final", func(t *testing.T) {
		var out bytes.Buffer
		turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
		turn.Close()
		closed := out.String()
		turn.Final("must not render")
		turn.Close()
		if out.String() != closed {
			t.Fatalf("closed turn accepted more output:\n%s", out.String())
		}
		if strings.Count(closed, "\n└") != 1 {
			t.Fatalf("early-close bottom border count = %d, want 1:\n%s", strings.Count(closed, "\n└"), closed)
		}
	})
}

func TestCardsRendererSuspendResumeCreatesClosedContinuation(t *testing.T) {
	var out bytes.Buffer
	turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLine("before handoff")
	turn.Suspend()
	if strings.Count(out.String(), "\n└") != 1 || strings.Count(out.String(), "│ └") != 1 {
		t.Fatalf("Suspend did not close the active card:\n%s", out.String())
	}

	turn.Resume()
	step.OutputLine("after handoff")
	step.Close()
	turn.Final("done")
	turn.Close()
	closed := out.String()
	turn.Close()
	if strings.Count(closed, "┌─ Shellia") != 2 || strings.Count(closed, "\n└") != 2 {
		t.Fatalf("continuation geometry is unbalanced:\n%s", closed)
	}
	if strings.Count(closed, "│ ┌─ step 1/1") != 2 || strings.Count(closed, "│ └") != 2 {
		t.Fatalf("nested continuation geometry is unbalanced:\n%s", closed)
	}
	assertOrdered(t, closed, "before handoff", "└", "Shellia · continued", "step 1/1 · continued", "after handoff")
	if out.String() != closed {
		t.Fatal("repeated Close changed resumed card output")
	}
}

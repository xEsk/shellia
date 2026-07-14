# Interactive Ctrl+C Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Ctrl+C exit only from the main interactive prompt while cancelling an active turn without losing session state.

**Architecture:** Keep signal ownership aligned with the active boundary. Non-interactive runs retain the application-level `NotifyContext`; interactive runs use an uncancelled session context and create `NotifyContext` only around turns. Raw terminal prompts translate byte `0x03` into either main-prompt exit or `context.Canceled` for prompts that belong to a turn.

**Tech Stack:** Go 1.26, standard `context`, `os/signal`, `testing`, and the existing `golang.org/x/term` terminal input implementation.

## Global Constraints

- Do not add dependencies or a global signal state machine.
- Preserve `/exit`, EOF, Ctrl+D, non-interactive exit code 130, history, and `/retry` behavior.
- Keep direct process dependencies inside `runtimeDeps` or thin process-level boundaries.
- Use focused RED/GREEN tests before production changes.

---

### Task 1: Separate interactive session and one-shot signal ownership

**Files:**
- Modify: `internal/app/main.go`
- Test: `internal/app/main_loop_test.go`

**Interfaces:**
- Consumes: `runApp(parentCtx context.Context, args []string, deps runtimeDeps) int`
- Produces: interactive `runInteractive` receives `parentCtx`; one-shot `runTurn` receives a `signal.NotifyContext(parentCtx, os.Interrupt)` context.

- [x] Add a focused regression test whose helper process starts an interactive turn, receives `os.Interrupt`, then accepts `/exit`; assert the process survives the interrupted turn, reports `Request cancelled.`, and reaches `Session closed.`
- [x] Run `env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run 'TestInteractiveSIGINTCancelsTurnWithoutClosingSession' -v` and verify RED because the first interrupt cancels the session context.
- [x] Move application-level signal registration to the non-interactive branch and pass `parentCtx` to `runInteractive`.
- [x] Re-run the focal test and verify GREEN.

### Task 2: Propagate raw Ctrl+C from turn-owned prompts

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_stepbox.go`
- Test: `internal/ui/ui_test.go`
- Test: `internal/ui/ui_stepbox_test.go`

**Interfaces:**
- Consumes: raw key byte returned by terminal input.
- Produces: `context.Canceled` from plan confirmation, planning-limit confirmation, step confirmation, and command editing when the key is `0x03`.

- [x] Add focused tests for a shared raw confirmation-key classifier: `0x03` returns `context.Canceled`; normal confirmation keys preserve their existing choices.
- [x] Cover command-editor raw control classification through the shared helper: `0x03` returns `context.Canceled` rather than an empty successful edit.
- [x] Run `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'Test.*CtrlC.*Cancel' -v` and verify RED because raw Ctrl+C is currently ignored or conflated with a normal cancellation choice.
- [x] Implement the smallest shared key classification helper and use it in the existing raw input paths; do not change normal key handling.
- [x] Re-run the focal tests and verify GREEN.

### Task 3: Boundary and repository verification

**Files:**
- Review: `internal/app/main.go`
- Review: `internal/ui/ui.go`
- Review: `internal/ui/ui_stepbox.go`
- Review: all changed tests

**Interfaces:**
- Consumes: completed Tasks 1 and 2.
- Produces: verified interactive cancellation behavior with unchanged one-shot behavior.

- [x] Run `gofmt -w internal/app/main.go internal/app/main_loop_test.go internal/ui/ui.go internal/ui/ui_test.go internal/ui/ui_stepbox.go`.
- [x] Run `env GOCACHE=/tmp/go-build go test -count=1 ./internal/app ./internal/ui`.
- [x] Run `env GOCACHE=/tmp/go-build go test -count=1 ./...`.
- [x] Run `env GOCACHE=/tmp/go-build go build -o /tmp/shellia-ctrl-c ./cmd/shellia`.
- [x] Review `git diff --check` and the complete diff for scope, cancellation wrapping, and preservation of unrelated files.

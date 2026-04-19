# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Shellia is a terminal-native AI shell agent CLI. It converts natural language instructions into inspectable shell commands, classifying each command's risk locally before execution and requiring per-command confirmation for anything risky or dangerous.

## Build & Development Commands

```bash
go build -o shellia .           # Build local binary
go run .                        # Interactive mode
go run . "run git status"       # One-shot mode
env GOCACHE=/tmp/go-build go test -count=1 ./...  # Run test suite in sandboxed environments
gofmt -w *.go                   # Format before opening a PR
```

Review `.goreleaser.yaml` and `.github/workflows/release.yml` if changing release behavior.

## Architecture

All code is in the `main` package at the repo root. No subdirectories for Go source.

**Core execution flow:**

```
main.go → parseArgs() → runInteractive() or one-shot
              ↓
          runTurn() [up to 4 rounds of planning]
              ├── callLLM()           [llm.go]   — HTTP request, streaming, retry
              ├── parseResponse()     [llm.go]   — JSON plan → commandPlan structs
              ├── executeCommands()   [executor.go] — PTY, capture, confirmation
              └── streamSummarize()   [llm.go]   — final answer stream
```

**File responsibilities:**

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point, arg parsing, interactive session loop, turn orchestration |
| `runtime.go` | Runtime dependency injection for core loops: stdio, HTTP client, command runners |
| `llm.go` | OpenAI-compatible API calls, prompt building, response parsing, streaming |
| `executor.go` | Command execution with PTY, bounded output capture, working directory tracking |
| `safety.go` + `safety_rules.go` | Local risk classification (safe/risky/dangerous) before any LLM trust |
| `config.go` | TOML config loading; precedence: defaults → `~/.shellia/config.toml` → env vars → CLI flags |
| `session_memory.go` | Session state across turns (pending intent, created files, runtime hints, observations) |
| `ui.go` + `ui_stepbox.go` | Terminal rendering, ANSI colors, step boxes, plan visualization |
| `input_unix.go` | Unix polling to distinguish Esc keypress from escape sequences |

**Key types:**

- `config` — merged configuration from all sources
- `runtimeDeps` — injectable process dependencies for core loop tests and orchestration
- `commandPlan` — LLM-generated plan for a single command (command, purpose, risk, interactive flag)
- `commandExecution` — post-execution result including captured stdout/stderr and exit code
- `sessionState` — rolling per-session memory for follow-up turns

**Runtime dependencies and tests:**

- `runInteractive`, `runTurn`, and executor entry points receive `runtimeDeps`.
- New loop or executor tests should inject temp files for `Stdin`, `Stdout`, `Stderr`, a fake `HTTPClient`, and fake runners when useful.
- Do not replace `os.Stdin`, `os.Stdout`, `os.Stderr`, or package-level HTTP state in new tests unless testing a true process-level wrapper.
- Keep direct `os.*` access in thin compatibility wrappers, terminal primitives, or process entry points. New core logic should accept `runtimeDeps`, `io.Reader`/`io.Writer`, or `*http.Client`.

**Safety classification pipeline** (local, not LLM-decided):
1. Shell operators (|, &, >, ;, `) → Risky
2. Dangerous roots (sudo, rm, dd, mkfs, shutdown, chown…) → Dangerous
3. Filesystem modification (mkdir, cp, tar…) → Risky
4. System/package managers (brew, apt, docker, npm…) → Risky
5. Known-safe allowlist (ls, pwd, cat, git status, docker inspect…) → Safe
6. Default → Risky

**Output capture** is bounded (configurable bytes) — stdout and stderr are captured separately with live streaming to terminal. Two capture thresholds: `observation_output_chars` (passed back to LLM for re-planning) and `summary_output_chars` (used in final summary).

## Coding Conventions

- Comments may be in English or Catalan — match the language of the surrounding file.
- Document new functions when the surrounding file does so.
- Add focused `_test.go` files near affected code using the standard `testing` package.
- Reuse existing helpers before adding abstractions.

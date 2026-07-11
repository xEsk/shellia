# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

IMPORTANT: When applicable, prefer using goland-index MCP tools for code navigation and refactoring.

## Project Overview

Shellia is a terminal-native AI shell agent CLI. It converts natural language instructions into inspectable shell commands, classifying each command's risk locally before execution and requiring per-command confirmation for anything risky or dangerous.

## Build & Development Commands


```bash
go build -o shellia ./cmd/shellia           # Build local binary
go run ./cmd/shellia                        # Interactive mode
go run ./cmd/shellia "run git status"       # One-shot mode
env GOCACHE=/tmp/go-build go test -count=1 ./...  # Run test suite in sandboxed environments
gofmt -w ./cmd ./internal                   # Format before opening a PR
```

Review `.goreleaser.yaml` and `.github/workflows/release.yml` if changing release behavior.

## Architecture

The executable entry point is in `cmd/shellia`. Private implementation packages live in `internal/`, following the same `cmd` + `internal` style used by `mcp-ai-memory`.

**Core execution flow:**

```
cmd/shellia/main.go → app.Run() → parseArgs() → runInteractive() or one-shot
                                      ↓
                                  runTurn() [configurable planning rounds]
                                      ├── llm        — HTTP request, streaming, retry, prompt parsing
                                      ├── safety     — local risk classification
                                      ├── executor   — PTY, capture, confirmation, cwd tracking
                                      └── ui         — terminal rendering and final answer stream display
```

**File responsibilities:**

| File | Responsibility |
|------|---------------|
| Path | Responsibility |
|------|---------------|
| `cmd/shellia/main.go` | Thin process entry point and build-time version injection |
| `internal/app` | Arg parsing, interactive session loop, turn orchestration, runtime dependency injection |
| `internal/core` | Shared types crossing package boundaries |
| `internal/config` | TOML config loading; precedence: defaults → `~/.config/shellia/config.toml` → env vars → CLI flags |
| `internal/llm` | OpenAI-compatible API calls, prompt building, response parsing, streaming |
| `internal/executor` | Command execution with PTY, bounded output capture, working directory tracking |
| `internal/safety` | Local risk classification (safe/risky/dangerous) before any LLM trust |
| `internal/session` | Session state across turns (pending intent, created files, runtime hints, observations) |
| `internal/ui` | Terminal rendering, ANSI colors, step boxes, plan visualization, prompt editor |
| `internal/trace` | JSONL diagnostic trace lifecycle and event payloads |
| `internal/interactive` | Slash-command parsing and completion |

**Key types:**

- `config.Config` — merged configuration from all sources
- `app.runtimeDeps` — injectable process dependencies for core loop tests and orchestration
- `core.CommandPlan` — LLM-generated plan for a single command (command, purpose, risk, interactive flag)
- `core.CommandExecution` — post-execution result including captured stdout/stderr and exit code
- `core.SessionState` — rolling per-session memory for follow-up turns

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

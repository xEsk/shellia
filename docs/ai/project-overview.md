# Project Overview

Read this for product behavior, runtime flow, provider integration, or session
semantics.

## Purpose and execution model

Shellia is a terminal-native AI shell agent. A user supplies an instruction; an
OpenAI-compatible provider returns a structured planning decision; Shellia locally
classifies, presents, confirms, and executes any admitted commands in the current
working directory. It then re-plans from bounded command evidence until completion
or a blocker.

`cmd/shellia/main.go` is deliberately thin: it sets the build-time version and calls
`app.Run`. `internal/app/main.go` parses configuration, obtains local context,
initializes renderer and optional trace output, and selects interactive or one-shot
execution.

## Workflow contract

`internal/app/turn.go` creates one `workflowState` per request. The provider response
uses an action (`execute`, `plan`, `retrieve_context`, `complete`, or `blocked`) and an
operation (`answer`, `observe`, `act`, or `capability`).

- `act` and `observe` can execute only when the workflow was not started by
  `--plan` or `/plan`.
- `answer` may retrieve explicitly referenced session results before completion.
- `answer+complete` may offer a separate plan-only objective.
- `capability+complete` may offer a separate executable objective.
- `act` and `observe` may return `plan`; it renders commands but never executes in
  that turn, and may offer a fresh executable workflow for later acceptance.
- Completion evidence is derived in `internal/app/workflow.go`: an action requires a
  successful current execution; an observation requires current observed evidence;
  answer/capability use their respective runtime-owned provenance.

Session-result retrieval is bounded to 16,000 runes in
`internal/app/context_retrieval.go` and only resolves current, referenced history
entries.

## Integrations and local data

- Provider: an OpenAI-compatible `/chat/completions` endpoint. Profiles can select
  `json_schema`, `json_object`, or prompt-only JSON guidance through configuration.
- Configuration: TOML at the XDG config path, with legacy `~/.shellia/config.toml`
  fallback; see `internal/config/config.go`.
- Context sent to the model can include cwd, user, OS, shell, and enabled session
  context. Repository Git state is intentionally excluded.
- Optional JSONL traces are written per session under the configured trace directory
  (default XDG state path) with restrictive directory/file permissions.

## Interactive behavior

`internal/app/interactive_loop.go` retains session history and routes normal prompts,
manual commands, and control commands. `internal/interactive/interactive_commands.go`
owns recognition for `/ai`, `/shell`, `/plan`, `/new`, `/retry`, `/context`, `/mode`,
`/model`, `/theme`, `/clear`, and `/exit`; a bare `exit` also exits.

Short-term state is updated in `internal/session/session_memory.go`. It tracks prior
results, observations, typed pending plan/execute offers, retry authority, and relevant files
only for the current interactive session.

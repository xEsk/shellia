# Shellia Operational Knowledge Base

Shellia is a Go CLI that converts natural-language requests into visible, locally
classified shell-command workflows. It supports interactive sessions and one-shot
runs, OpenAI-compatible planning providers, local confirmation, bounded evidence
and follow-up session context.

## Stack and architecture

- Go 1.26 module (`github.com/xEsk/shellia`); standard `testing` package.
- Binary entry point: `cmd/shellia/main.go`.
- `internal/app` owns CLI orchestration, the interactive loop, and the workflow.
- Domain packages isolate configuration, LLM transport/validation, command
  execution, safety classification, session state, tracing, terminal UI, and
  slash-command parsing.
- `site/` is a separate static GitHub Pages site; it is not part of the CLI.

## Primary subsystems

- App and workflow: `internal/app/` — start here for behavior spanning planning,
  execution, session state, and presentation.
- Provider contract: `internal/llm/` — OpenAI-compatible Chat Completions request,
  structured response schema, and decision validation.
- Local execution boundary: `internal/executor/` and `internal/safety/` — command
  confirmation, process lifecycle, output capture, and authoritative risk checks.
- User interaction: `internal/ui/`, `internal/interactive/`, and
  `internal/session/` — prompts, visual styles, controls, and short-term memory.
- Configuration and diagnostics: `internal/config/` and `internal/trace/`.

## Implementation philosophy

- Preserve user control: the model proposes; runtime safety and confirmation decide
  what may execute.
- Keep planning evidence bounded and causal. Completion is validated by the runtime,
  not merely asserted by the provider.
- Keep dependencies injectable through `runtimeDeps` in app-loop tests.
- Prefer narrow domain changes over cross-package abstractions.

## Critical constraints

- `--plan` and `/plan` have no execution authority.
- Never treat model risk labels, repeat reasons, or completion claims as authority;
  `internal/safety` and `internal/app/workflow.go` are authoritative.
- Git state is not ambient prompt context. Request it through an explicit command.
- Trace files may contain prompts, raw provider responses, and command output; they
  are diagnostic data, not general logging.
- The GoReleaser target and build entry point are `./cmd/shellia`.

## Task routing

- Product behavior and runtime flows: [project overview](project-overview.md)
- Finding the right package or operational file: [repository map](repository-map.md)
- Setup, commands, tests, and extension patterns: [development guide](development-guide.md)
- Safety-sensitive workflow semantics: [workflow and safety](workflow-and-safety.md)
- Non-negotiable project-specific boundaries: [critical rules](critical-rules.md)

Open only the documents relevant to the current task, then verify material claims in
current source and tests.

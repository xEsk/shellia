# Critical Rules

Read this before touching execution, planning, provider transport, configuration, or
release behavior.

- Never add an execution path that bypasses `internal/safety` classification and the
  existing executor confirmation flow. `--yes-safe` applies only after local
  classification.
- Do not let provider output create execution authority, permit arbitrary repeats, or
  satisfy completion without runtime-owned causal evidence. Preserve plan-only's zero
  execution authority.
- Do not restore ambient Git probing, cached Git context, an `include_git` setting in
  generated configuration, or Git fields in prompt context. Ask for focused Git
  commands when needed.
- Do not reintroduce a session-wide `os.Interrupt` listener for interactive sessions
  or kill only a shell parent when cancelling a non-interactive command; both regress
  established Ctrl+C and descendant-cleanup semantics.
- Treat trace output as sensitive diagnostics. It can contain full prompts, raw LLM
  responses, and captured command output; tracing remains opt-in.
- Preserve configuration compatibility: unknown legacy TOML keys are tolerated, while
  new defaults belong in both `defaultConfig` and `defaultConfigTemplate` when the
  setting is persisted.
- Keep tests for app loops dependency-injected with `runtimeDeps`; process globals are
  only for thin production entry points.
- If release behavior changes, update and verify both `.goreleaser.yaml` and the
  relevant GitHub workflow. The release build entry point is `./cmd/shellia`.

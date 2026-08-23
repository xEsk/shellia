# Repository Map

Read this to locate implementation ownership before editing.

| Task | Start here | Why / related locations |
| --- | --- | --- |
| CLI startup, flags, exit codes, interactive dispatch | `cmd/shellia/main.go`, `internal/app/main.go` | The command binary is a wrapper; `runApp` owns configuration, trace lifetime, and mode selection. |
| Planning loop, outcome semantics, evidence, no-progress behavior | `internal/app/turn.go`, `internal/app/workflow.go`, `internal/app/context_retrieval.go` | These files own one workflow's authority and causal completion checks. |
| App loop tests or dependency injection | `internal/app/runtime.go`, `internal/app/*_test.go` | Use `runtimeDeps`; do not replace process-global stdin/stdout/stderr or HTTP state. |
| TOML, environment overrides, model profiles, visual preferences | `internal/config/config.go`, `internal/config/visual_style.go` | Config defaults, validation, persistence, and generated template live here. |
| Provider HTTP, prompt shape, response schema, decoding | `internal/llm/llm.go`, `internal/llm/prompt.go`, `internal/llm/response.go` | Keep protocol and strict/compatible parsing in this package. |
| Planned/manual command execution, PTY/plain behavior, output capture | `internal/executor/executor.go`, `internal/executor/writers.go` | Execution has its own runtime options and presentation adapters. |
| Command risk and shell syntax | `internal/safety/safety.go`, `internal/safety/safety_rules.go` | This is the authoritative local classifier; tests include adversarial substitutions. |
| Interactive controls and session follow-ups | `internal/interactive/`, `internal/session/`, `internal/app/interactive_loop.go` | Parser is isolated; app routes actions; session package owns retained state. |
| Terminal visual presentation | `internal/ui/` | `visual_renderer.go` selects plain, guide, bands, or cards implementations. |
| Trace format and storage | `internal/trace/` | Optional session JSONL traces, IDs, stream metadata, and storage paths. |
| Public site | `site/index.html`, `site/script.js` | Static site deployed separately by the Pages workflow. |
| CI, releases, deploys | `.github/workflows/`, `.goreleaser.yaml` | CI builds/tests/vets/lints/race-tests; tagged releases run GoReleaser; `site/**` changes deploy Pages. |
| Historical design decisions | `docs/superpowers/specs/`, `docs/superpowers/plans/` | Use as background only; source and tests are authoritative. |

Shared cross-domain contracts are in `internal/core/types.go`. Put a type there only
when it is genuinely shared; otherwise keep it with the owning domain.

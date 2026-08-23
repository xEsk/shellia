# Workflow and Safety Guide

Read this before changing planning, command execution, confirmations, retry logic,
shell syntax classification, or session evidence.

## Authority boundaries

The model proposes a response, but it cannot grant itself execution authority.
`workflowState` derives whether execution is allowed once from plan-only mode and
does not expose a setter. Semantic repair may not move a decision between executable
(`act`/`observe`) and non-executable (`answer`/`capability`) operation groups.

For an `execute` decision, `internal/llm` normalizes model commands and
`internal/safety.ClassifyCommand` independently determines the effective risk and
confirmation requirement. `internal/executor` rechecks edited or repeated commands
before execution. A successful exact command needs a runtime-authorized repeat cause;
the provider's text alone is insufficient.

## Completion and evidence

`internal/app/workflow.go` owns causal completion checks. Do not make completion
depend on untrusted provider metadata or interpretation of arbitrary output:

- `act`: requires a successful execution in the latest workflow batch.
- `observe`: requires current observed command evidence, with a narrow validated retry
  exception for matching prior observation state.
- `answer`: uses model knowledge or explicitly retrieved session-result context.
- `capability`: is non-executing and can expose an explicit pending proposal.

Each workflow caps structural repairs at three malformed decisions. Planning follow-up
rounds default to five and can be configured with
`[execution] planning_max_rounds` or `SHELLIA_PLANNING_MAX_ROUNDS`.

## Execution and cancellation

The executor distinguishes normal failures, timeouts, cancellation, interactive
commands, and steps independent on failure. On Unix-like non-interactive command
runs, it creates a process group and kills that group on cancellation so descendants
do not remain running. Keep this behavior scoped to the existing non-interactive
path unless PTY semantics are deliberately analyzed.

Interactive Ctrl+C cancels the active turn or prompt and returns to the main prompt;
one-shot Ctrl+C ends the application with exit code 130. Preserve that boundary when
adding cancellation handling.

## Shell classifier

`internal/safety/safety.go` recognizes shell operators and command roots, including
executable `$()` and backtick substitutions. Substitutions are active even inside
double quotes; single-quoted or escaped markers are literal. Dangerous roots hidden
in substitutions must retain dangerous classification. Extend its table-driven tests
for syntax changes rather than adding an external shell parser casually.

## Trace-data warning

When tracing is enabled, the JSONL session trace includes prompts, raw provider
responses, planning decisions, confirmations, commands, and captured output. Keep it
opt-in, preserve `0700` trace directories and `0600` trace files, and do not expose
it as ordinary user-facing output.

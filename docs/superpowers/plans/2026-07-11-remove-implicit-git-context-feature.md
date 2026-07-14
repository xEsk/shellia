# Remove Implicit Git Context — Feature Specification

**Status:** Proposed; requires approval before implementation  
**Scope:** MEDIUM — one behavior removal crossing core types, configuration, execution state, LLM prompts, terminal UI, tests, README, and the public website, without new infrastructure or dependencies.

## 1. Goal and observable outcome

Shellia must stop detecting, storing, displaying, tracing, or automatically sending Git repository state to the planning model.

After this change:

- starting Shellia does not execute Git commands;
- changing the session working directory does not execute Git commands;
- planning and discovery-repair prompts contain no implicit `git.is_repo`, `git.branch`, or `git.status_short` fields;
- `/context`, debug output, and turn headers contain no detected Git state;
- when a task depends on repository state, the model obtains current information by proposing an explicit read-only Git inspection command through the existing plan, safety, capture, observation, and re-planning flow;
- README and public website copy describe Git inspection as explicit and on demand, not as ambient context.

The value is a smaller and more predictable prompt, no unbounded `git status --short` capture, fresher task-scoped Git observations, and less unnecessary disclosure of repository metadata.

## 2. Current behavior and reusable contracts

`executor.getContext` currently detects the working directory, user, OS, shell, and Git state. Git discovery calls `git rev-parse`, `git branch --show-current`, and `git status --short` through `runCommandCapture`, whose `CombinedOutput` result is unbounded. `applySessionState` repeats Git discovery after a successful recognized `cd`.

`core.ContextInfo` stores that data in `GitContext`. `llm.buildPromptContextBlock` adds it to every planning prompt and discovery-repair prompt when `IncludeGit` is enabled. `ui.printContextTo` and the compact header render it. Configuration exposes `context.include_git`, enabled by default.

The replacement behavior already exists and must be reused:

- the planning rules support short local discovery commands and observation-driven re-planning;
- read-only Git inspection subcommands are classified locally by the existing safety rules;
- command stdout/stderr pass through bounded `CapturedStream` capture;
- observation and summary prompts apply their configured character budgets and truncation strategy;
- successful `cd` commands already update `ContextInfo.CWD` independently of Git refresh.

No new Git service, cache, context field, prompt budget, configuration option, or dependency is needed.

## 3. Acceptance Contract

### Initiating actions

The contract applies when a user starts Shellia in interactive, one-shot, or plan-only mode; changes directories through a recognized successful `cd`; opens `/context`; enables debug or verbose rendering; or asks for a task whose answer depends on Git state.

### Visible and API results

1. Startup and directory changes preserve the current working directory behavior but perform no automatic Git subprocess calls.
2. `ContextInfo` contains only the remaining local context fields; no Git field or Git-specific shared type remains.
3. Runtime and file configuration expose no `IncludeGit` or `include_git` setting, and newly generated config files omit it.
4. Normal planning and discovery-repair prompts never contain the removed Git field names or automatically captured repository values.
5. `/context`, debug context, and compact headers continue to render enabled non-Git fields without Git, branch, clean, dirty, or status labels derived from ambient detection.
6. A request such as “show me the Git status” can still produce an explicit `git status` inspection plan. Its output follows the existing safety classification, confirmation policy, capture limits, observation limits, and summarization flow.
7. Existing configuration files containing `context.include_git` continue to load without error. The obsolete key has no effect and is not written by `shellia config init`.
8. README and active website copy no longer claim that Git state is detected, refreshed, shared implicitly, or preserved as ambient context. Explicit Git command demonstrations remain valid.

### Durable proof

No persistence or migration is introduced. Durable proof consists of focused automated tests, the generated config template, and active documentation containing no obsolete Git-context contract.

### Failure and recovery

Removing Git detection removes its lookup and subprocess failure modes. Explicit Git inspection failures use the existing command error and re-planning behavior. Shellia must not silently reintroduce ambient Git probing as a fallback.

### Out of scope

- changing Git command safety classification;
- changing confirmation rules for explicit Git commands;
- changing command capture, observation, memory, or summary limits;
- adding special prompt rules that force Git inspection for every repository-related request;
- persisting a shell process environment or changing the existing `cd` parser;
- redesigning `/context` beyond removing Git-specific output;
- modifying release automation or historical files under `docs/old/`.

## 4. In scope / Out of scope

### In scope

- remove `core.GitContext` and `ContextInfo.Git`;
- remove package aliases that exist only for `GitContext`;
- remove `Config.IncludeGit`, `FileConfig.Context.IncludeGit`, its default, merge logic, and generated TOML entry;
- remove `getGitContext` and `runCommandCapture` if semantic reference checks confirm they have no non-Git callers;
- simplify `getContext` and `applySessionState` while preserving all non-Git behavior;
- remove Git rendering from planning prompts, discovery-repair prompts, `/context`, debug output, and compact headers;
- update or replace Git-context tests with negative contract tests and preserved-CWD tests;
- update README and active `docs/index.html` copy;
- retain explicit `git status`, `git log`, and similar demonstrations where they show commands chosen or entered by the user/model.

### Out of scope

The exclusions in the Acceptance Contract are binding. In particular, this feature does not attempt to improve unrelated website claims, restructure configuration, or change session-memory semantics.

## 5. Data flow and authority

### Remaining ambient context

```text
process/runtime facts
  -> executor.getContext
  -> core.ContextInfo {cwd, user, os, shell}
  -> LLM planning context and local UI, subject to existing context flags
```

### Git state on demand

```text
user instruction requiring Git state
  -> planning model proposes a focused Git inspection command
  -> local safety classifier assigns the authoritative risk
  -> existing confirmation policy decides execution
  -> executor captures bounded stdout/stderr
  -> observation is returned to the next planning round or final summary
```

The model has no direct authority to inspect the repository. It may only propose commands. Shellia's local classifier and confirmation flow remain authoritative. Repository paths and status are shared with the configured provider only when an explicit executed observation is included in a prompt, under the existing capture and prompt limits.

## 6. Ordered behavior slices

### Slice 1 — Remove Git from the context contract

**State behavior:** Local context represents process/runtime facts only; Git state has no shared type or configuration flag.  
**Boundary:** `internal/core`, package aliases, and `internal/config`.  
**Focal RED:** Tests expect no `Git` member in serialized/context construction paths, no `IncludeGit` default or merge behavior, no `include_git` in generated config, and successful loading of a legacy config containing the obsolete key.  
**GREEN:** Remove the Git types and configuration wiring while preserving all other context defaults and legacy-file loading.  
**Dependency:** None.

### Slice 2 — Remove automatic Git subprocesses while preserving `cwd`

**State behavior:** Startup and successful simple `cd` update only the remaining context and never probe Git. Failed or unsupported directory changes remain unchanged.  
**Boundary:** `internal/executor` context collection and session-state update.  
**Focal RED:** Executor tests prove a successful `cd` changes `CWD` without Git-dependent state and that startup context collection succeeds independently of Git availability or repository membership. Semantic search establishes that `runCommandCapture` has no remaining callers before deletion.  
**GREEN:** Remove Git lookup/refresh and delete now-unused helpers/imports without altering command execution.  
**Dependency:** Slice 1.

### Slice 3 — Remove Git from prompts and terminal UI

**State behavior:** Every planning channel and context display contains only enabled non-Git context.  
**Boundary:** `internal/llm` prompt construction and `internal/ui` rendering.  
**Focal RED:** Prompt tests assert normal and discovery-repair prompts lack `git.is_repo`, `git.branch`, and `git.status_short`; UI tests assert `/context` and headers render `cwd` and other enabled fields without Git-derived labels.  
**GREEN:** Delete Git prompt and rendering branches, Git header helpers, and obsolete tests while preserving empty-context fallback behavior.  
**Dependency:** Slice 1.

### Slice 4 — Prove explicit, bounded Git inspection still works

**State behavior:** Git-dependent requests use the ordinary command-observation loop instead of ambient state.  
**Boundary:** planner request/response integration, safety classification, executor capture, and re-planning.  
**Focal RED:** Add or adapt a focused app-loop test whose fake planning response proposes a read-only Git inspection, whose injected executor returns an observation, and whose next request contains that bounded observation but no ambient Git fields.  
**GREEN:** Existing architecture should satisfy this after fixture updates; production changes are allowed only if the focal test exposes a real missing connection.  
**Dependency:** Slices 2 and 3.

**Earliest useful walking-skeleton checkpoint:** Slices 1–3 complete, with a planning prompt and `/context` output demonstrably free of Git data while `cd` still updates `cwd`. Slice 4 then proves the intended replacement path end to end.

### Slice 5 — Align README and public website

**State behavior:** Product documentation promises current runtime context plus explicit, observable inspection—not automatic Git-state sharing.  
**Boundary:** `README.md`, active `docs/index.html`, and any active `docs/script.js` demonstrations.  
**Focal RED:** Repository search identifies active claims such as “Git repository context,” “Git context is refreshed,” `git state` in ambient context, and `git context refreshes`.  
**GREEN:** Replace those claims with accurate language about `cwd`, runtime context, bounded captured observations, and on-demand inspection. Keep explicit Git commands in demos because they illustrate the supported replacement behavior. Do not edit `docs/old/`.  
**Dependency:** Acceptance wording from Slices 1–4.

## 7. Applicable risks and recovery

- **Planner quality regression:** The first plan may need an additional read-only Git inspection round. This is intentional and uses the existing discovery/re-planning contract. Verify with Slice 4 rather than restoring ambient Git state.
- **Configuration compatibility:** Users may retain `include_git` in existing TOML files. The current decoder tolerates unknown keys; lock this down with a focused test. If implementation reveals strict decoding elsewhere, stop and choose an explicit deprecation path before proceeding.
- **UI snapshot drift:** Removing branch/clean/dirty text changes headers and `/context`. Update only assertions and copy tied to the removed contract.
- **Scope leakage:** `git` remains a supported command family and appears legitimately in examples and safety rules. Searches must distinguish implicit context from explicit command usage.
- **Dirty worktree overlap:** `internal/app/main.go` and `internal/app/main_loop_test.go` already contain unrelated local modifications. Implementation must inspect and preserve them, applying minimal patches around current content.

Recovery is a normal source rollback; there is no stored data migration or external state change.

## 8. Verification strategy

### Focal development checks

- `env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app -run 'Test.*Config|Test.*Git.*Observation'`
- `env GOCACHE=/tmp/go-build go test -count=1 ./internal/executor -run 'TestGetContext|TestApplySessionState'`
- `env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm -run 'TestBuild.*Prompt.*Context|TestBuildDiscoveryRepairPrompt'`
- `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'TestPrintHeader|TestPrintContext'`

Exact test names may follow current package naming, but each Acceptance Contract item must map to an assertion.

### Affected-boundary checkpoint

- Run `env GOCACHE=/tmp/go-build go test -count=1 ./internal/app ./internal/config ./internal/core ./internal/executor ./internal/llm ./internal/ui ./internal/trace`.
- Search active source and docs for `GitContext`, `IncludeGit`, `include_git`, `getGitContext`, `git.status_short`, and claims that Git context is detected or refreshed. Remaining matches must be explicit command examples, safety behavior, legacy compatibility fixtures, or this specification.
- Inspect captured fake LLM request bodies to prove neither normal planning nor discovery repair contains ambient Git fields.

### Visible and integration proof

- Build a temporary binary with `go build -o /tmp/shellia-remove-git-context ./cmd/shellia`.
- Run focused fake-provider/app-loop coverage showing an explicit Git inspection observation reaches re-planning through bounded capture.
- Render or serve the active website using the repository's existing lightweight workflow and visually inspect the changed context, modes, and “How it works” sections at desktop and narrow widths. No layout redesign is expected.

### Final gates

- `gofmt -w` only changed Go files.
- `env GOCACHE=/tmp/go-build go test -count=1 ./...`
- `go build -o /tmp/shellia-remove-git-context ./cmd/shellia`
- `git diff --check`
- Review the complete diff for unrelated refactors, historical-doc edits, and accidental changes to explicit Git command support.

## 9. Documentation and decision impact

README updates must cover Basic features, How it works, persistent shell-mode state, configuration, and privacy/behavior wording wherever ambient Git context is promised.

The active website must update at least:

- the animated “reading context” sequence;
- the “reads context” pillar;
- direct-mode “State preserved” copy;
- shell-loop shared-context copy;
- “How it works / Local context.”

Explicit Git command examples in the prompt-mode demo, shell-loop terminal, classifier, and `docs/script.js` should remain unless their surrounding prose falsely describes them as automatic context. They demonstrate the approved on-demand model.

This is a product-contract decision: Git state is task-scoped observed command output, never ambient startup/session context. After implementation and verification, record that decision in canonical project memory with current file references. Do not update memory during design because the behavior is not implemented yet.

## 10. Stop Gates and implementation handoff

Stop and request a decision only if implementation discovers one of these conditions:

1. existing config decoding rejects obsolete `include_git` keys instead of ignoring them;
2. a non-prompt/non-UI runtime contract depends on `GitContext` beyond the mapped references;
3. preserving explicit Git inspection requires changing safety authority or confirmation semantics;
4. website changes require a broader content or layout redesign rather than bounded copy edits;
5. unrelated local changes overlap the exact lines needed and cannot be preserved safely.

Otherwise this feature is ready for `implement-project-feature` after user approval. Implementation must use focused RED/GREEN tests, make the smallest cross-module removal, preserve unrelated worktree changes, and complete the verification gates above.

# Repository Guidelines

## Project Structure & Module Organization

Shellia is a small Go CLI structured like a standard application module. The binary entry point lives in `cmd/shellia/main.go`. Private application code lives under `internal/`: orchestration in `internal/app`, shared cross-domain types in `internal/core`, configuration in `internal/config`, command execution in `internal/executor`, safety classification in `internal/safety`, model integration in `internal/llm`, terminal rendering in `internal/ui`, session follow-up memory in `internal/session`, traces in `internal/trace`, and slash-command parsing in `internal/interactive`.

Technical design and implementation documents live in `docs/`. Static website files for the public docs/release page live in `site/` (`index.html`, `script.js`). Release automation is defined in `.github/workflows/release.yml`, and GitHub Pages deployment is defined in `.github/workflows/pages.yml`.

IMPORTANT: When applicable, prefer using goland-index MCP tools for code navigation and refactoring.

## Build, Test, and Development Commands

- `go build -o shellia ./cmd/shellia` builds the local CLI binary.
- `go run ./cmd/shellia` starts Shellia in interactive mode.
- `go run ./cmd/shellia "run git status"` runs a one-shot instruction locally.
- `env GOCACHE=/tmp/go-build go test -count=1 ./...` runs the Go test suite in sandboxed environments. Add and run tests with every behavioral change.
- `gofmt -w ./cmd ./internal` formats Go source before opening a PR.

If you change release behavior, also review `.goreleaser.yaml` and the GitHub workflow.

## Coding Style & Naming Conventions

Follow standard Go formatting and keep implementations simple. Use tabs as produced by `gofmt`, `camelCase` for unexported names, and short, descriptive file names grouped by responsibility (`executor.go`, `safety.go`, etc.).

Document every new method or function when the surrounding file does so. Match the language and tone already used in that file: some comments are in English, others in Catalan. Reuse existing helpers before adding new abstractions.

## Testing Guidelines

Use Go’s standard `testing` package and name tests with `TestXxx`. Add focused tests near the affected code for parser, safety, config, session-state, UI formatting, and loop behavior.

Core loop tests should use `runtimeDeps` from `runtime.go` instead of replacing process globals. Inject temporary `Stdin`, `Stdout`, `Stderr`, fake `HTTPClient`, and runner functions through `runtimeDeps`; avoid assigning to `os.Stdin`, `os.Stdout`, `os.Stderr`, or package-level HTTP state in new tests.

When adding new core code, thread dependencies through `runtimeDeps` or explicit `io.Reader`/`io.Writer`/`*http.Client` parameters. Keep direct `os.Stdout`/`os.Stdin` usage inside thin compatibility wrappers or true process-level entry points only.

## Commit & Pull Request Guidelines

Recent history uses short, imperative commit messages such as `Fixed bug with "you" prompt` and `Updated public site`. Keep commits focused and descriptive; one concern per commit when possible.

Pull requests should explain the user-visible change, note any risk in command execution or safety behavior, and include terminal output or screenshots when UI or `site/` content changes.

## Contributor Notes

Prefer straightforward solutions, avoid re-implementing existing logic, and ask before making risky or unclear changes. Keep contributor changes aligned with the current CLI-first architecture.

## Project Memory MCP

- Canonical project key: `shellia`
- Always use this value as `project` in `mcp__project_memory` tool calls.

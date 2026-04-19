# Repository Guidelines

## Project Structure & Module Organization

Shellia is a small Go CLI kept mostly at the repository root. Core entry and flow live in `main.go`, runtime dependency wiring in `runtime.go`, command execution in `executor.go`, safety classification in `safety.go` plus `safety_rules.go`, model integration in `llm.go`, configuration in `config.go`, and terminal rendering in `ui.go` plus `ui_stepbox.go`. Session follow-up memory is handled in `session_memory.go`.

Static website files for the public docs/release page live in `docs/` (`index.html`, `styles.css`, `script.js`). Release automation is defined in `.github/workflows/release.yml`.

## Build, Test, and Development Commands

- `go build -o shellia .` builds the local CLI binary.
- `go run .` starts Shellia in interactive mode.
- `go run . "run git status"` runs a one-shot instruction locally.
- `env GOCACHE=/tmp/go-build go test -count=1 ./...` runs the Go test suite in sandboxed environments. Add and run tests with every behavioral change.
- `gofmt -w *.go` formats root Go files before opening a PR.

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

Pull requests should explain the user-visible change, note any risk in command execution or safety behavior, and include terminal output or screenshots when UI or `docs/` content changes.

## Contributor Notes

Prefer straightforward solutions, avoid re-implementing existing logic, and ask before making risky or unclear changes. Keep contributor changes aligned with the current CLI-first architecture.

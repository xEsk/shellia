# Repository Guidelines

## Project Structure & Module Organization

Shellia is a small Go CLI kept mostly at the repository root. Core entry and flow live in `main.go`, command execution in `executor.go`, safety classification in `safety.go`, model integration in `llm.go`, configuration in `config.go`, and terminal rendering in `ui.go` plus `ui_stepbox.go`. Session follow-up memory is handled in `session_memory.go`.

Static website files for the public docs/release page live in `docs/` (`index.html`, `styles.css`, `script.js`). Release automation is defined in `.github/workflows/release.yml`.

## Build, Test, and Development Commands

- `go build -o shellia .` builds the local CLI binary.
- `go run .` starts Shellia in interactive mode.
- `go run . "run git status"` runs a one-shot instruction locally.
- `go test ./...` runs the Go test suite. Add and run tests with every behavioral change.
- `gofmt -w *.go` formats root Go files before opening a PR.

If you change release behavior, also review `.goreleaser.yaml` and the GitHub workflow.

## Coding Style & Naming Conventions

Follow standard Go formatting and keep implementations simple. Use tabs as produced by `gofmt`, `camelCase` for unexported names, and short, descriptive file names grouped by responsibility (`executor.go`, `safety.go`, etc.).

Document every new method or function when the surrounding file does so. Match the language and tone already used in that file: some comments are in English, others in Catalan. Reuse existing helpers before adding new abstractions.

## Testing Guidelines

There are currently no committed `*_test.go` files, so new features and bug fixes should add focused tests near the affected code. Use Go’s standard `testing` package and name tests with `TestXxx`. Prefer small unit tests for parser, safety, config, and session-state logic.

## Commit & Pull Request Guidelines

Recent history uses short, imperative commit messages such as `Fixed bug with "you" prompt` and `Updated public site`. Keep commits focused and descriptive; one concern per commit when possible.

Pull requests should explain the user-visible change, note any risk in command execution or safety behavior, and include terminal output or screenshots when UI or `docs/` content changes.

## Contributor Notes

Prefer straightforward solutions, avoid re-implementing existing logic, and ask before making risky or unclear changes. Keep contributor changes aligned with the current CLI-first architecture.

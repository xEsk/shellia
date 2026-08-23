# Development Guide

Read this before implementing or validating a change.

## Prerequisites and commands

- The module declares Go 1.26 in `go.mod`.
- Build: `go build -o shellia ./cmd/shellia`
- Run interactively: `go run ./cmd/shellia`
- Run one instruction: `go run ./cmd/shellia "run git status"`
- Test: `env GOCACHE=/tmp/go-build go test -count=1 ./...`
- Format Go source: `gofmt -w ./cmd ./internal`
- The equivalent convenience targets are in `Makefile`.

CI additionally runs `go vet ./...`, golangci-lint, and `go test -race -count=1 ./...`.
Run focused package tests while iterating, then the full suite for behavior changes.

## Extension patterns

- Add CLI orchestration in `internal/app`; keep `cmd/shellia/main.go` thin.
- Keep persistent setting parsing, defaults, validation, and config-template changes
  together in `internal/config`. A generated config must reflect changed defaults.
- Add provider-wire behavior through `internal/llm`; validate model decisions before
  workflow code relies on them.
- Add command semantics through `internal/executor` and classify executable effects
  through `internal/safety`. Never bypass the latter for a new execution route.
- Add a control command to `internal/interactive/` first, then route it from the
  interactive app loop and cover parser plus loop behavior.
- Add visual behavior behind the existing renderer facade, not scattered output-style
  conditionals.

## Test conventions

- Use standard Go `testing` with `TestXxx` names and package-local tests.
- Core-loop tests inject temporary files, HTTP clients, and runners through
  `runtimeDeps` (`internal/app/runtime.go`). Do not assign `os.Stdin`, `os.Stdout`,
  `os.Stderr`, or package-global HTTP state.
- Put focused behavior coverage beside the owner: config, safety, executor, UI,
  session, LLM, or app loop.

## Release and site changes

- `.goreleaser.yaml` builds `./cmd/shellia` for Linux and Darwin on amd64/arm64 and
  injects `main.version`.
- `.github/workflows/release.yml` runs tests and vet before `goreleaser release` on
  version tags.
- `site/**` changes on `main` trigger `.github/workflows/pages.yml`. Review both the
  static files and deployment workflow when changing public-site behavior.

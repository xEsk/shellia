# Interactive Theme Selector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent `/theme` selector for the `plain`, `guide`, `bands`, and `cards` visual styles and apply the selected renderer immediately in the interactive session.

**Architecture:** Extend the existing `/model` command path across the current `interactive`, `config`, `ui`, and `app` owners. `internal/config` remains the source of truth for valid visual styles and updates only `[ui].style`; `internal/app` persists first and then atomically swaps the in-memory config and renderer using `effectivePresentation`.

**Tech Stack:** Go, standard `testing` package, BurntSushi TOML-compatible text files, existing Shellia renderer facade.

## Global Constraints

- Persist every successful selection to `[ui].style`; the change is never session-only.
- Always expose exactly `plain`, `guide`, `bands`, and `cards`, including the unfinished visual style.
- Reuse the existing slash-command menu and renderer facade; add no dependencies or alternate prompt flow.
- Preserve `--no-color`, `TERM=dumb`, non-TTY fallback, config comments, config file permissions, session history, and interactive mode.
- Invalid names or persistence errors must leave both `cfg.VisualStyle` and `deps.Renderer` unchanged.
- Do not modify `internal/ui/visual_renderer.go`, any `visual_<theme>.go` file, their tests, or `internal/app/main_loop_test.go`; those files contain unrelated in-progress user changes.

## File Map

- `internal/interactive/interactive_commands.go`: recognize `/theme` and parse its optional argument.
- `internal/interactive/export.go`: expose the new command and parser to `internal/app`.
- `internal/interactive/interactive_command_test.go`: parser regression tests.
- `internal/config/visual_style.go`: canonical ordered list of selectable styles.
- `internal/config/config.go`: precise `[ui].style` update and persistence.
- `internal/config/export.go`: expose style enumeration and persistence.
- `internal/config/visual_style_test.go`: visual-style and TOML persistence tests.
- `internal/ui/ui_commandmenu.go`: `/theme` submenu, active marker, filtering, and completion.
- `internal/ui/ui_commandmenu_test.go`: menu and completion tests.
- `internal/app/aliases.go`: package aliases for the new cross-package contracts.
- `internal/app/main.go`: list, validate, persist, and apply theme changes in the interactive loop.
- `internal/app/theme_test.go`: isolated application and loop tests, avoiding the dirty `main_loop_test.go`.

---

### Task 1: Parse `/theme` as a local interactive command

**Files:**
- Modify: `internal/interactive/interactive_commands.go`
- Modify: `internal/interactive/export.go`
- Test: `internal/interactive/interactive_command_test.go`

**Interfaces:**
- Produces: `CommandTheme`, `ParseThemeCommandName(input string) string`.
- Consumed by: Task 4 through `internal/app/aliases.go`.

- [ ] **Step 1: Add failing parser tests**

Extend `TestParseInteractiveCommandSlashCommands` with:

```go
{name: "theme", input: "/theme", want: interactiveCommandTheme},
{name: "theme argument", input: "/theme cards", want: interactiveCommandTheme},
```

Add:

```go
// TestParseThemeCommandName extracts the selected visual style from /theme.
func TestParseThemeCommandName(t *testing.T) {
	if got := parseThemeCommandName(" /theme CARDS "); got != "CARDS" {
		t.Fatalf("parseThemeCommandName() = %q, want CARDS", got)
	}
	if got := parseThemeCommandName("/theme"); got != "" {
		t.Fatalf("parseThemeCommandName(/theme) = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/interactive
```

Expected: compilation fails because `interactiveCommandTheme` and `parseThemeCommandName` do not exist.

- [ ] **Step 3: Implement the minimal parser and exports**

In `interactive_commands.go`, add the constant and command spec:

```go
interactiveCommandTheme interactiveCommand = "theme"
```

```go
{Input: "/theme", Command: interactiveCommandTheme, Description: "switch visual theme"},
```

Allow arguments for both local selectors:

```go
acceptsArgument := spec.Command == interactiveCommandModel || spec.Command == interactiveCommandTheme
if normalized == spec.Input || (acceptsArgument && strings.HasPrefix(normalized, spec.Input+" ")) {
	return spec.Command
}
```

Add:

```go
// parseThemeCommandName extracts the optional visual style from a /theme command.
func parseThemeCommandName(input string) string {
	trimmed := strings.TrimSpace(input)
	fields := strings.Fields(trimmed)
	if len(fields) < 2 || strings.ToLower(fields[0]) != "/theme" {
		return ""
	}
	return fields[1]
}
```

In `export.go`, expose:

```go
CommandTheme = interactiveCommandTheme
```

```go
// ParseThemeCommandName extracts the visual style from `/theme <name>`.
func ParseThemeCommandName(input string) string {
	return parseThemeCommandName(input)
}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/interactive
```

Expected: PASS.

- [ ] **Step 5: Commit the parser slice**

```bash
git add internal/interactive/interactive_commands.go internal/interactive/export.go internal/interactive/interactive_command_test.go
git commit -m "Add theme slash command parser"
```

---

### Task 2: Persist the selected style inside `[ui]`

**Files:**
- Modify: `internal/config/visual_style.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/export.go`
- Test: `internal/config/visual_style_test.go`

**Interfaces:**
- Produces: `VisualStyles() []VisualStyle`, `PersistVisualStyle(cfg Config, style VisualStyle) error`, and internal `updateVisualStyleTOML(content string, style VisualStyle) string`.
- Consumed by: Task 3 for menu rows and Task 4 for persistent selection.

- [ ] **Step 1: Add failing enumeration and TOML tests**

Add to `visual_style_test.go`:

```go
func TestVisualStylesListsEverySelectableStyle(t *testing.T) {
	want := []VisualStyle{VisualStylePlain, VisualStyleGuide, VisualStyleBands, VisualStyleCards}
	if got := visualStyles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("visualStyles() = %#v, want %#v", got, want)
	}
}

func TestUpdateVisualStyleTOMLReplacesStyleInsideUI(t *testing.T) {
	input := "default_model = \"openai\"\n\n[ui]\n# keep this comment\nstyle = \"plain\"\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	want := "default_model = \"openai\"\n\n[ui]\n# keep this comment\nstyle = \"cards\"\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	if got := updateVisualStyleTOML(input, VisualStyleCards); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}

func TestUpdateVisualStyleTOMLInsertsMissingUIStyle(t *testing.T) {
	input := "default_model = \"openai\"\n\n[ui]\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	want := "default_model = \"openai\"\n\n[ui]\nno_color = false\nstyle = \"guide\"\n\n[context]\ninclude_cwd = true\n"
	if got := updateVisualStyleTOML(input, VisualStyleGuide); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}

func TestUpdateVisualStyleTOMLAppendsMissingUISection(t *testing.T) {
	input := "default_model = \"openai\"\n"
	want := "default_model = \"openai\"\n\n[ui]\nstyle = \"bands\"\n"
	if got := updateVisualStyleTOML(input, VisualStyleBands); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}
```

Add `reflect` to the test imports.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config
```

Expected: compilation fails because `visualStyles` and `updateVisualStyleTOML` do not exist.

- [ ] **Step 3: Implement the canonical list and precise TOML updater**

In `visual_style.go` add:

```go
// visualStyles returns every selectable visual style in menu order.
func visualStyles() []VisualStyle {
	return []VisualStyle{
		VisualStylePlain,
		VisualStyleGuide,
		VisualStyleBands,
		VisualStyleCards,
	}
}
```

In `config.go` add:

```go
// persistVisualStyle writes the selected visual style to [ui].style.
func persistVisualStyle(cfg Config, style VisualStyle) error {
	normalized := normalizeVisualStyle(string(style), "")
	if normalized == "" {
		return fmt.Errorf("unknown visual style %q", strings.TrimSpace(string(style)))
	}
	path := strings.TrimSpace(cfg.ConfigPath)
	if path == "" {
		return fmt.Errorf("cannot persist ui.style: no config file was loaded")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot inspect config file: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read config file: %w", err)
	}
	updated := updateVisualStyleTOML(string(data), normalized)
	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot write config file: %w", err)
	}
	return nil
}

// updateVisualStyleTOML updates only the style assignment inside [ui].
func updateVisualStyleTOML(content string, style VisualStyle) string {
	line := "style = " + strconv.Quote(string(style))
	lines := strings.Split(content, "\n")
	uiStart := -1
	uiEnd := len(lines)

	for index, current := range lines {
		trimmed := strings.TrimSpace(current)
		if trimmed == "[ui]" {
			uiStart = index
			continue
		}
		if uiStart >= 0 && strings.HasPrefix(trimmed, "[") {
			uiEnd = index
			break
		}
	}

	if uiStart < 0 {
		if content == "" {
			return "[ui]\n" + line + "\n"
		}
		separator := "\n\n"
		if strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + "[ui]\n" + line + "\n"
	}

	for index := uiStart + 1; index < uiEnd; index++ {
		key, _, ok := strings.Cut(strings.TrimSpace(lines[index]), "=")
		if ok && strings.TrimSpace(key) == "style" {
			lines[index] = line
			return strings.Join(lines, "\n")
		}
	}

	lines = append(lines, "")
	copy(lines[uiEnd+1:], lines[uiEnd:])
	lines[uiEnd] = line
	return strings.Join(lines, "\n")
}
```

In `export.go` add:

```go
// VisualStyles returns every selectable terminal visual style in menu order.
func VisualStyles() []VisualStyle {
	return visualStyles()
}

// PersistVisualStyle writes the selected terminal visual style to [ui].style.
func PersistVisualStyle(cfg Config, style VisualStyle) error {
	return persistVisualStyle(cfg, style)
}
```

- [ ] **Step 4: Add and pass the real persistence test**

Add:

```go
func TestPersistVisualStyleKeepsConfigBodyAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := "[ui]\nstyle = \"plain\"\n\n[context]\ninclude_cwd = true\n"
	if err := os.WriteFile(path, []byte(input), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg := defaultConfig()
	cfg.ConfigPath = path
	if err := persistVisualStyle(cfg, VisualStyleCards); err != nil {
		t.Fatalf("persistVisualStyle() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "[ui]\nstyle = \"cards\"\n\n[context]\ninclude_cwd = true\n" {
		t.Fatalf("config = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("permissions = %o, want 640", got)
	}
}
```

Add `os` and `path/filepath` to the imports, then run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config
```

Expected: PASS.

- [ ] **Step 5: Commit the config slice**

```bash
git add internal/config/visual_style.go internal/config/config.go internal/config/export.go internal/config/visual_style_test.go
git commit -m "Persist interactive visual theme"
```

---

### Task 3: Add the four-theme submenu and Tab completion

**Files:**
- Modify: `internal/ui/ui_commandmenu.go`
- Test: `internal/ui/ui_commandmenu_test.go`

**Interfaces:**
- Consumes: `configpkg.VisualStyles() []configpkg.VisualStyle` directly from Task 2.
- Produces: menu rendering and completion for `/theme` without new exported UI APIs.

- [ ] **Step 1: Add failing menu tests**

Add:

```go
// TestCommandMenuLinesShowsVisualThemes renders all four themes and marks the active one.
func TestCommandMenuLinesShowsVisualThemes(t *testing.T) {
	cfg := defaultConfig()
	cfg.VisualStyle = configpkg.VisualStyleBands
	got := commandMenuLines(false, "/theme ", cfg)
	if len(got) != 6 {
		t.Fatalf("commandMenuLines(/theme) returned %d lines, want 6", len(got))
	}
	joined := strings.Join(got, "\n")
	for _, name := range []string{"plain", "guide", "* bands", "cards"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("commandMenuLines(/theme) = %#v, missing %q", got, name)
		}
	}
}

// TestCompleteInteractiveCommandCompletesVisualTheme checks Tab completion for /theme.
func TestCompleteInteractiveCommandCompletesVisualTheme(t *testing.T) {
	got, ok := completeInteractiveCommand("/theme ca", defaultConfig())
	if !ok || got != "/theme cards" {
		t.Fatalf("completeInteractiveCommand(/theme ca) = %q, %t; want /theme cards, true", got, ok)
	}
}
```

Import `configpkg "shellia/internal/config"`.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'Test(CommandMenuLinesShowsVisualThemes|CompleteInteractiveCommandCompletesVisualTheme)'
```

Expected: FAIL because `/theme` has no submenu or completion.

- [ ] **Step 3: Implement the theme submenu**

Import the config package in `ui_commandmenu.go` and add:

```go
// themeMenuPrefix extracts the style prefix when the prompt is editing /theme.
func themeMenuPrefix(input string) (string, bool) {
	if input == "" || strings.TrimLeft(input, " \t") != input || strings.ContainsAny(input, "\r\n") {
		return "", false
	}
	lower := strings.ToLower(input)
	switch {
	case lower == "/theme":
		return "", true
	case strings.HasPrefix(lower, "/theme "):
		fields := strings.Fields(input)
		if len(fields) > 2 {
			return "", false
		}
		return strings.TrimSpace(input[len("/theme"):]), true
	default:
		return "", false
	}
}

// themeMenuSuggestions renders selectable visual styles for the /theme submenu.
func themeMenuSuggestions(prefix string, cfg config) []commandMenuItem {
	descriptions := map[configpkg.VisualStyle]string{
		configpkg.VisualStylePlain: "classic Shellia output",
		configpkg.VisualStyleGuide: "guided rail layout",
		configpkg.VisualStyleBands: "full-width band layout",
		configpkg.VisualStyleCards: "bordered card layout",
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	styles := configpkg.VisualStyles()
	suggestions := make([]commandMenuItem, 0, len(styles))
	for _, visualStyle := range styles {
		name := string(visualStyle)
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		if visualStyle == cfg.VisualStyle {
			name = "* " + name
		}
		suggestions = append(suggestions, commandMenuItem{Input: name, Description: descriptions[visualStyle]})
	}
	return suggestions
}
```

Check `themeMenuPrefix` before `modelMenuPrefix` in `commandMenuSuggestions`. Add the equivalent first-match loop before model completion in `completeInteractiveCommand`:

```go
if prefix, ok := themeMenuPrefix(input); ok {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, visualStyle := range configpkg.VisualStyles() {
		name := string(visualStyle)
		if prefix == "" || strings.HasPrefix(name, prefix) {
			return "/theme " + name, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run the UI package and verify GREEN**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui
```

Expected: PASS.

- [ ] **Step 5: Commit the menu slice**

```bash
git add internal/ui/ui_commandmenu.go internal/ui/ui_commandmenu_test.go
git commit -m "Add visual theme command menu"
```

---

### Task 4: Persist and apply the selected renderer immediately

**Files:**
- Modify: `internal/app/aliases.go`
- Modify: `internal/app/main.go`
- Create: `internal/app/theme_test.go`

**Interfaces:**
- Consumes: `interactivepkg.CommandTheme`, `interactivepkg.ParseThemeCommandName`, `configpkg.NormalizeVisualStyle`, `configpkg.VisualStyles`, `configpkg.PersistVisualStyle`, `effectivePresentation`, and `newRenderer`.
- Produces: `switchInteractiveTheme(cfg *config, deps *runtimeDeps, user string, name string) error` and local `/theme` handling in `runInteractive`.

- [ ] **Step 1: Add failing atomic-switch tests in a new file**

Create `internal/app/theme_test.go`:

```go
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "shellia/internal/config"
)

func TestSwitchInteractiveThemePersistsAndReplacesRenderer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"plain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = stdout.Close() })

	cfg := defaultConfig()
	cfg.ConfigPath = path
	cfg.NoColor = true
	deps := defaultRuntimeDeps()
	deps.Stdout = stdout
	deps.StdoutIsTerminal = func(*os.File) bool { return true }
	deps.Renderer = newRenderer(stdout, presentation{Style: configpkg.VisualStylePlain})
	previous := deps.Renderer

	if err := switchInteractiveTheme(&cfg, &deps, "test-user", "CARDS"); err != nil {
		t.Fatalf("switchInteractiveTheme() error = %v", err)
	}
	if cfg.VisualStyle != configpkg.VisualStyleCards {
		t.Fatalf("VisualStyle = %q, want cards", cfg.VisualStyle)
	}
	if deps.Renderer == previous {
		t.Fatal("renderer was not replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "cards"`) {
		t.Fatalf("config = %q, want cards", data)
	}
}

func TestSwitchInteractiveThemeFailureKeepsConfigAndRenderer(t *testing.T) {
	cfg := defaultConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "missing.toml")
	deps := defaultRuntimeDeps()
	deps.Renderer = newRenderer(deps.Stdout, presentation{Style: configpkg.VisualStylePlain})
	previous := deps.Renderer

	if err := switchInteractiveTheme(&cfg, &deps, "test-user", "guide"); err == nil {
		t.Fatal("switchInteractiveTheme() error = nil, want persistence failure")
	}
	if cfg.VisualStyle != configpkg.VisualStylePlain || deps.Renderer != previous {
		t.Fatal("failed switch changed runtime state")
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run 'TestSwitchInteractiveTheme'
```

Expected: compilation fails because `switchInteractiveTheme` does not exist.

- [ ] **Step 3: Add aliases and the atomic switch helper**

In `aliases.go`, add:

```go
interactiveCommandTheme = interactivepkg.CommandTheme
```

```go
parseThemeCommandName = interactivepkg.ParseThemeCommandName
persistVisualStyle    = configpkg.PersistVisualStyle
normalizeVisualStyle  = configpkg.NormalizeVisualStyle
visualStyles          = configpkg.VisualStyles
```

In `main.go`, add:

```go
// switchInteractiveTheme persists and applies one visual style between turns.
func switchInteractiveTheme(cfg *config, deps *runtimeDeps, user string, name string) error {
	selected := normalizeVisualStyle(name, "")
	if selected == "" {
		return fmt.Errorf("visual theme %q not found", strings.TrimSpace(name))
	}

	next := *cfg
	next.VisualStyle = selected
	if err := persistVisualStyle(next, selected); err != nil {
		return fmt.Errorf("theme was not changed: %w", err)
	}

	effective := effectivePresentation(next, *deps)
	nextRenderer := newRenderer(deps.Stdout, presentation{
		Style: effective.Style,
		ANSI:  effective.ANSI,
		User:  user,
	})
	*cfg = next
	deps.Renderer = nextRenderer
	return nil
}
```

Run the focused test again; expected: PASS.

- [ ] **Step 4: Add failing loop tests for listing and local selection**

Add to `theme_test.go`:

```go
func TestRunInteractiveThemeCommandListsAndSwitchesWithoutLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"plain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.NoColor = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "/theme\n/theme cards\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.StdoutIsTerminal = func(*os.File) bool { return true }
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})
	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
	for _, text := range []string{"* plain", "guide", "bands", "cards", "Theme switched to cards."} {
		if !strings.Contains(output, text) {
			t.Fatalf("output = %q, missing %q", output, text)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "cards"`) {
		t.Fatalf("config = %q, want cards", data)
	}
}

func TestRunInteractiveUnknownThemeKeepsCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"guide\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.VisualStyle = configpkg.VisualStyleGuide
	ctxInfo := loopTestContext(t)
	output := captureMainLoopIO(t, "/theme missing\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})
	if !strings.Contains(output, `visual theme "missing" not found`) {
		t.Fatalf("output = %q, want unknown-theme warning", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "guide"`) {
		t.Fatalf("config = %q, want unchanged guide", data)
	}
}
```

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run 'TestRunInteractive.*Theme'
```

Expected: FAIL because `runInteractive` does not handle `interactiveCommandTheme`.

- [ ] **Step 5: Wire `/theme` into the loop**

Add beside the `/model` case:

```go
case interactiveCommandTheme:
	themeName := parseThemeCommandName(trimmed)
	if themeName == "" {
		printVisualThemesTo(deps.Stdout, ui, cfg)
		continue
	}
	if err := switchInteractiveTheme(&cfg, &deps, ctxInfo.User, themeName); err != nil {
		printWarningTo(deps.Stderr, ui, err.Error())
		continue
	}
	printInfoTo(deps.Stdout, ui, "Theme switched to "+string(cfg.VisualStyle)+".")
	continue
```

Add the local list renderer near `printModelProfilesTo`:

```go
// printVisualThemesTo lists every selectable visual style and marks the configured one.
func printVisualThemesTo(target io.Writer, ui bool, cfg config) {
	lines := make([]string, 0, len(visualStyles()))
	for _, visualStyle := range visualStyles() {
		marker := " "
		if visualStyle == cfg.VisualStyle {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, visualStyle))
	}
	renderPanel(target, ui, "themes", colorCyan, lines)
}
```

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run 'Test(SwitchInteractiveTheme|RunInteractive.*Theme)'
```

Expected: PASS.

- [ ] **Step 6: Commit the application slice**

```bash
git add internal/app/aliases.go internal/app/main.go internal/app/theme_test.go
git commit -m "Apply interactive visual theme changes"
```

---

### Task 5: Final formatting and regression verification

**Files:**
- Verify all files changed in Tasks 1-4.
- Do not stage or alter the pre-existing dirty renderer/spec/test files listed in Global Constraints.

**Interfaces:**
- Consumes: the complete `/theme` feature.
- Produces: formatted, fully tested, buildable repository state.

- [ ] **Step 1: Format only changed Go files**

```bash
gofmt -w internal/interactive/interactive_commands.go internal/interactive/export.go internal/interactive/interactive_command_test.go internal/config/visual_style.go internal/config/config.go internal/config/export.go internal/config/visual_style_test.go internal/ui/ui_commandmenu.go internal/ui/ui_commandmenu_test.go internal/app/aliases.go internal/app/main.go internal/app/theme_test.go
```

- [ ] **Step 2: Run package-level feature tests**

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/interactive ./internal/config ./internal/ui ./internal/app
```

Expected: PASS with zero failures.

- [ ] **Step 3: Run the full regression suite**

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./...
```

Expected: PASS with zero failures.

- [ ] **Step 4: Build the CLI**

```bash
env GOCACHE=/tmp/go-build go build -o /tmp/shellia-theme-selector ./cmd/shellia
```

Expected: exit code 0.

- [ ] **Step 5: Inspect scope and whitespace**

```bash
git diff --check
git status --short
git diff -- internal/interactive internal/config internal/ui/ui_commandmenu.go internal/ui/ui_commandmenu_test.go internal/app/aliases.go internal/app/main.go internal/app/theme_test.go
```

Expected: no whitespace errors; only the planned feature files differ from their task commits, while the pre-existing user changes remain unstaged and untouched.

- [ ] **Step 6: Commit formatting corrections only if needed**

If Step 1 changed files after their task commits:

```bash
git add internal/interactive/interactive_commands.go internal/interactive/export.go internal/interactive/interactive_command_test.go internal/config/visual_style.go internal/config/config.go internal/config/export.go internal/config/visual_style_test.go internal/ui/ui_commandmenu.go internal/ui/ui_commandmenu_test.go internal/app/aliases.go internal/app/main.go internal/app/theme_test.go
git commit -m "Format interactive theme selector"
```

# Terminal Visual Styles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implementar els estils configurables `plain`, `guide`, `bands` i `cards` de Shellia, fidels a la referència visual aprovada i sense canviar el workflow, la seguretat ni el streaming.

**Architecture:** `internal/config` serà propietari del valor persistent; `internal/app` resoldrà una sola presentació efectiva a partir de config, TTY i `TERM`; `internal/ui` exposarà una façana `Renderer`/`Turn` sobre quatre implementacions aïllades; `internal/executor` alimentarà el torn actiu amb passos i output incremental. Els renderers compartiran només contractes i primitives mecàniques: cap estil importarà, incrustarà ni cridarà un altre estil.

**Tech Stack:** Go 1.26, `golang.org/x/term`, `io.Writer`, proves amb `testing`; cap dependència nova ni alternate screen.

## Global Constraints

- Especificació canònica: `docs/superpowers/specs/2026-08-07-terminal-visual-styles-design.md`.
- Referència visual canònica: `docs/superpowers/specs/assets/terminal-visual-styles-reference.html`.
- `plain` és el default i ha de preservar la sortida actual.
- `--no-color` elimina ANSI de Shellia però conserva la geometria configurada.
- stdout no-TTY o `TERM=dumb` força `plain` sense ANSI.
- El PTY interactiu queda fora de guies, bandes i targetes mentre controla el terminal.
- `cards` és incremental; no pot bufferitzar tot l’output fins al final.
- No s’afegeixen flags, variables d’entorn, slash commands ni configuració de colors.
- No hi haurà sistema de plugins dinàmics ni condicionals d’estil fora del selector i del renderer propietari.
- Les proves noves injectaran `runtimeDeps`, streams i detecció TTY; no substituiran `os.Stdout`, `os.Stdin` ni `os.Stderr`.

---

## Outcome, scope i proporcionalitat

Nivell `BOUNDED`: és comportament visible i regression-worthy dins l’arquitectura existent, sense persistència nova, migracions, autoritat ni dependències. Queden fora d’abast una TUI full-screen, temes cromàtics, una API pública de plugins, canvis al planner/safety i el redisseny general d’`internal/ui`.

El primer checkpoint útil és `plain` passant pel nou contracte sense drift. Després cada estil és una slice vertical independent i rebutjable per separat. `cards` i el handoff PTY tenen checkpoints propis perquè concentren el risc d’estat incremental.

## Mapa de fitxers

- Crear `internal/config/visual_style.go`: tipus, constants i normalització dels quatre valors.
- Modificar `internal/config/config.go` i `internal/config/export.go`: default, TOML, merge i export del normalitzador.
- Crear `internal/app/presentation.go`: resolució única de `Style` i `ANSI`.
- Modificar `internal/app/runtime.go`, `internal/app/main.go` i `internal/app/aliases.go`: detector TTY injectat, renderer i torn actiu.
- Crear `internal/ui/visual_renderer.go`: façanes, contractes interns i selector únic.
- Crear `internal/ui/visual_surface.go`: width, wrapping, prefixes, ANSI i superfícies incrementals compartides.
- Crear `internal/ui/visual_plain.go`, `visual_guide.go`, `visual_bands.go`, `visual_cards.go`: geometria exclusiva de cada estil.
- Modificar `internal/ui/ui.go`, `ui_stepbox.go` i `export.go`: delegació semàntica i `stepBox` alimentat per una superfície.
- Modificar `internal/executor/export.go`, `aliases.go`, `executor.go` i `writers.go`: transportar el torn, streaming i suspensió PTY.
- Crear `*_test.go` al costat de cada nou owner; modificar només els tests d’integració afectats.
- Modificar `README.md`: documentar `[ui].style` i la interacció amb `--no-color`/no-TTY.

## Contractes que queden fixats pel pla

`internal/config/visual_style.go`:

```go
type VisualStyle string

const (
	VisualStylePlain VisualStyle = "plain"
	VisualStyleGuide VisualStyle = "guide"
	VisualStyleBands VisualStyle = "bands"
	VisualStyleCards VisualStyle = "cards"
)

func normalizeVisualStyle(value string, fallback VisualStyle) VisualStyle
```

`internal/ui/visual_renderer.go`:

```go
type Presentation struct {
	Style configpkg.VisualStyle
	ANSI  bool
}

type Renderer struct {
	impl rendererImpl
	ansi bool
}

type Turn struct {
	impl turnImpl
}

type rendererImpl interface {
	userTurn(mode core.InteractiveMode, text string)
	beginShelliaTurn(configpkg.Config, core.ContextInfo) turnImpl
}

type turnImpl interface {
	plan(configpkg.Config, string, []core.CommandPlan, bool)
	beginStep(configpkg.Config, int, int, core.CommandPlan) *stepBox
	final(string)
	suspend()
	resume()
	close()
}

func NewRenderer(io.Writer, Presentation) *Renderer
func (r *Renderer) UserTurn(core.InteractiveMode, string)
func (r *Renderer) BeginShelliaTurn(configpkg.Config, core.ContextInfo) *Turn
func (t *Turn) Plan(configpkg.Config, string, []core.CommandPlan, bool)
func (t *Turn) BeginStep(configpkg.Config, int, int, core.CommandPlan) *StepBox
func (t *Turn) Final(string)
func (t *Turn) Suspend()
func (t *Turn) Resume()
func (t *Turn) Close()
```

`Renderer` i `Turn` són façanes concretes perquè `app` i `executor` no coneguin les implementacions. `rendererImpl` i `turnImpl` romanen interns a `ui`. El selector exhaustiu de `NewRenderer` és l’únic `switch` sobre l’estil.

### Task 1: Configuració i presentació efectiva

**Files:**
- Create: `internal/config/visual_style.go`
- Create: `internal/config/visual_style_test.go`
- Create: `internal/app/presentation.go`
- Create: `internal/app/presentation_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/export.go`
- Modify: `internal/app/runtime.go`

**Interfaces:**
- Produces: `config.VisualStyle`, `config.NormalizeVisualStyle`, `ui.Presentation` inputs i `effectivePresentation(cfg, deps)`.
- Dependency: cap.

- [ ] **Step 1: Escriure els tests RED de configuració**

```go
func TestNormalizeVisualStyle(t *testing.T) {
	tests := []struct{ input string; want VisualStyle }{
		{" plain ", VisualStylePlain},
		{"GUIDE", VisualStyleGuide},
		{"bands", VisualStyleBands},
		{"cards", VisualStyleCards},
		{"unknown", VisualStyleGuide},
	}
	for _, tt := range tests {
		if got := normalizeVisualStyle(tt.input, VisualStyleGuide); got != tt.want {
			t.Fatalf("normalizeVisualStyle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVisualStyleConfigContract(t *testing.T) {
	cfg := defaultConfig()
	if cfg.VisualStyle != VisualStylePlain { t.Fatalf("default style = %q", cfg.VisualStyle) }
	fileCfg := FileConfig{}
	fileCfg.UI.Style = " cards "
	applyFileConfig(&cfg, fileCfg)
	if cfg.VisualStyle != VisualStyleCards { t.Fatalf("merged style = %q", cfg.VisualStyle) }
	if !strings.Contains(defaultConfigTemplate(), "style = \"plain\"") { t.Fatal("template lacks ui.style") }
}
```

- [ ] **Step 2: Executar el RED focalitzat**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/config -run 'TestNormalizeVisualStyle|TestVisualStyleConfigContract'`

Expected: FAIL perquè `VisualStyle`, `UI.Style` i `Config.VisualStyle` encara no existeixen.

- [ ] **Step 3: Implementar el contracte mínim de config**

Afegir les quatre constants, `strings.TrimSpace` + minúscules, `VisualStyle: VisualStylePlain` al default, `Style string \`toml:"style"\`` a `FileConfig.UI`, merge només quan el valor no és buit i `style = "plain"` amb els quatre valors comentats a la plantilla. Exportar `NormalizeVisualStyle` des d’`export.go`.

- [ ] **Step 4: Escriure els tests RED de la matriu efectiva**

```go
func TestEffectivePresentation(t *testing.T) {
	tests := []struct {
		name string; style configpkg.VisualStyle; noColor, tty bool; term string
		wantStyle configpkg.VisualStyle; wantANSI bool
	}{
		{"tty cards", configpkg.VisualStyleCards, false, true, "xterm-256color", configpkg.VisualStyleCards, true},
		{"no color guide", configpkg.VisualStyleGuide, true, true, "xterm", configpkg.VisualStyleGuide, false},
		{"pipe", configpkg.VisualStyleCards, false, false, "xterm", configpkg.VisualStylePlain, false},
		{"dumb", configpkg.VisualStyleBands, false, true, "dumb", configpkg.VisualStylePlain, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", tt.term)
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil { t.Fatal(err) }
			defer stdout.Close()
			cfg := defaultConfig()
			cfg.VisualStyle = tt.style
			cfg.NoColor = tt.noColor
			deps := runtimeDeps{
				Stdout: stdout,
				StdoutIsTerminal: func(*os.File) bool { return tt.tty },
			}
			got := effectivePresentation(cfg, deps)
			if got.Style != tt.wantStyle || got.ANSI != tt.wantANSI {
				t.Fatalf("effectivePresentation() = %#v, want style=%q ANSI=%t", got, tt.wantStyle, tt.wantANSI)
			}
		})
	}
}
```

- [ ] **Step 5: Implementar `effectivePresentation` i el detector injectat**

Afegir a `runtimeDeps`:

```go
StdoutIsTerminal func(*os.File) bool
Renderer         *uipkg.Renderer
Turn             *uipkg.Turn
```

El default de `StdoutIsTerminal` delega a `term.IsTerminal(int(file.Fd()))`. `effectivePresentation` usa `deps.Stdout`, conserva l’estil amb `NoColor`, i força `plain`/sense ANSI només per no-TTY o `TERM=dumb`.

- [ ] **Step 6: Executar tests i commit**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app -run 'Test.*VisualStyle|TestEffectivePresentation'`

Expected: PASS.

Commit: `Add terminal visual style configuration`

### Task 2: Contracte de renderer i regressió `plain`

**Files:**
- Create: `internal/ui/visual_renderer.go`
- Create: `internal/ui/visual_surface.go`
- Create: `internal/ui/visual_plain.go`
- Create: `internal/ui/visual_renderer_test.go`
- Create: `internal/ui/visual_plain_test.go`
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_stepbox.go`
- Modify: `internal/ui/export.go`
- Modify: `internal/app/main.go`
- Modify: `internal/app/aliases.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/executor/export.go`
- Modify: `internal/executor/aliases.go`
- Modify: `internal/executor/executor.go`

**Interfaces:**
- Consumes: `config.VisualStyle` i `effectivePresentation` de Task 1.
- Produces: les façanes `ui.Renderer`, `ui.Turn` i `Turn.BeginStep` usades per app/executor.

- [ ] **Step 1: Escriure una fixture semàntica i el RED de selecció**

```go
func TestNewRendererSelectsEveryImplementation(t *testing.T) {
	for _, style := range []configpkg.VisualStyle{
		configpkg.VisualStylePlain, configpkg.VisualStyleGuide,
		configpkg.VisualStyleBands, configpkg.VisualStyleCards,
	} {
		var out bytes.Buffer
		renderer := NewRenderer(&out, Presentation{Style: style, ANSI: false})
		if renderer == nil { t.Fatalf("NewRenderer(%q) = nil", style) }
	}
}
```

La fixture comuna ha de cridar `UserTurn`, `BeginShelliaTurn`, `Plan`, `BeginStep`, `OutputLabel`, `OutputLine`, `Final` i `Close` amb el cas real `df -h /`.

Definir-la a `visual_renderer_test.go` perquè tots els estils comparteixin exactament el mateix input, no expectatives ni implementació:

```go
func testConfig() configpkg.Config {
	cfg := configpkg.DefaultConfig()
	cfg.Model = "gpt-5.4-mini"
	return cfg
}

func testPlan() core.CommandPlan {
	return core.CommandPlan{Command: "df -h /", Purpose: "Mostrar l'espai lliure.", Risk: "safe"}
}

func newTestTurn(out io.Writer, style configpkg.VisualStyle, ansi bool) *Turn {
	r := NewRenderer(out, Presentation{Style: style, ANSI: ansi})
	return r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
}

func renderConversationFixture(t *testing.T, style configpkg.VisualStyle, ansi bool) string {
	t.Helper()
	var out bytes.Buffer
	r := NewRenderer(&out, Presentation{Style: style, ANSI: ansi})
	r.UserTurn(core.InteractiveModeAI, "quant d'espai queda al disc?")
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
	turn.Plan(testConfig(), "Cal consultar l'espai disponible.", []core.CommandPlan{testPlan()}, false)
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("419Gi available")
	step.Close()
	turn.Final("Queden 419Gi lliures al disc arrel (/).")
	turn.Close()
	return out.String()
}

func assertOrdered(t *testing.T, output string, values ...string) {
	t.Helper()
	position := 0
	for _, value := range values {
		next := strings.Index(output[position:], value)
		if next < 0 { t.Fatalf("output lacks ordered value %q:\n%s", value, output) }
		position += next + len(value)
	}
}
```

- [ ] **Step 2: Executar el RED**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'TestNewRenderer|TestPlainRenderer'`

Expected: FAIL perquè les façanes i el selector no existeixen.

- [ ] **Step 3: Crear façanes i primitives compartides**

Implementar exactament els contractes fixats a l’inici del pla. `visual_surface.go` pot contenir width, wrapping, prefix, row i lifecycle incremental; no pot contenir noms ni switches de `plain`, `guide`, `bands` o `cards`.

Adaptar `stepBox` perquè escrigui a una superfície creada pel torn, mantenint els seus mètodes actuals (`Command`, `Bullet`, `Text`, `Section`, `KeyValue`, `OutputLabel`, `OutputLine`, `EditCommand`, `ReplaceLastRenderedRow`, `IsClosed`, `Close`). `newStepBox` continua creant la superfície plain per als callers no migrats.

- [ ] **Step 4: Implementar `plainRenderer` amb els helpers actuals**

`visual_plain.go` és l’únic owner de la geometria actual. Ha de reutilitzar `printSubmittedPromptTo`, `printHeaderTo`, `printPlanTo`, `printCommandExecutionTo` i `printFinalResultTo`, o moure’n el cos sense modificar bytes visibles. No incrusta cap altre renderer.

- [ ] **Step 5: Connectar el lifecycle sense canviar la sortida**

Després de `parseArgs`, `runApp` resol la presentació una vegada i crea `deps.Renderer`. `readInteractivePrompt` usa `Renderer.UserTurn` només en mode AI després de netejar el prompt transitori. `runTurn` fa:

```go
turnUI := deps.Renderer.BeginShelliaTurn(cfg, *ctxInfo)
defer turnUI.Close()
deps.Turn = turnUI
```

Els plans i finals passen per `turnUI`; `executorDeps` transporta `Turn`, i `executeCommands` crea passos amb `deps.Turn.BeginStep`. Si tests criden directament `runTurn` o executor amb renderer nil, el boundary crea un renderer plain amb l’ANSI bool rebut.

- [ ] **Step 6: Provar regressió plain i app loop**

Afegir una assertion byte-for-byte de la fixture plain i una prova `runInteractive` amb stdin/stdout injectats que contingui, en ordre, `you ›`, `Shellia`, `plan`, `step 1/1`, `system output` i resposta final.

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui ./internal/app ./internal/executor`

Expected: PASS sense actualitzar assertions no relacionades.

- [ ] **Step 7: Commit**

Commit: `Route plain output through visual renderer`

### Task 3: Slice vertical `guide`

**Files:**
- Create: `internal/ui/visual_guide.go`
- Create: `internal/ui/visual_guide_test.go`
- Modify: `internal/ui/visual_renderer.go` només per afegir el mapping `VisualStyleGuide -> newGuideRenderer`.

**Interfaces:**
- Consumes: `rendererImpl`, `turnImpl` i primitives de Task 2.
- Produces: guia cian d’usuari, guia magenta de Shellia i guia neutra niada d’execució.

- [ ] **Step 1: Escriure els tests RED de jerarquia**

```go
func TestGuideRendererNestsTechnicalActivity(t *testing.T) {
	output := renderConversationFixture(t, configpkg.VisualStyleGuide, false)
	for _, want := range []string{"│ Tu", "│ you › quant d'espai", "│ Shellia", "│   plan", "│   step 1/1", "│     system output"} {
		if !strings.Contains(output, want) { t.Fatalf("guide output lacks %q:\n%s", want, output) }
	}
}

func TestGuideNoColorKeepsGeometryWithoutANSI(t *testing.T) {
	output := renderConversationFixture(t, configpkg.VisualStyleGuide, false)
	if strings.Contains(output, "\033[") || !strings.Contains(output, "│") { t.Fatalf("output = %q", output) }
}
```

- [ ] **Step 2: Executar el RED**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'TestGuide'`

Expected: FAIL perquè el selector encara cau a `plain` o no existeix `newGuideRenderer`.

- [ ] **Step 3: Implementar només `visual_guide.go`**

El fitxer és propietari dels prefixes, indentació i separadors de `guide`. Ha d’usar les primitives compartides per wrapping i ANSI, però no `plainRenderer`, `bandsRenderer` ni `cardsRenderer`. Els separadors globals apareixen només al tancament del torn.

- [ ] **Step 4: Verificar i commit**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui ./internal/app -run 'TestGuide|TestRunInteractive'`

Expected: PASS.

Commit: `Add guide conversation renderer`

### Task 4: Slice vertical `bands`

**Files:**
- Create: `internal/ui/visual_bands.go`
- Create: `internal/ui/visual_bands_test.go`
- Modify: `internal/ui/visual_renderer.go` només per al mapping `VisualStyleBands`.

**Interfaces:**
- Consumes: contracte/primitives de Task 2.
- Produces: marcador lateral ample i fons ANSI subtil per torn; fallback geomètric sense ANSI.

- [ ] **Step 1: Escriure els tests RED**

```go
func TestBandsRendererOwnsWholeTurns(t *testing.T) {
	output := renderConversationFixture(t, configpkg.VisualStyleBands, true)
	if !strings.Contains(output, "▌") { t.Fatalf("bands marker missing: %q", output) }
	if !strings.Contains(output, "\033[48;") { t.Fatalf("bands background missing: %q", output) }
	assertOrdered(t, output, "Tu", "Shellia", "plan", "step 1/1", "system output")
}

func TestBandsNoColorUsesMarkerNotBackground(t *testing.T) {
	output := renderConversationFixture(t, configpkg.VisualStyleBands, false)
	if strings.Contains(output, "\033[") || !strings.Contains(output, "▌") { t.Fatalf("output = %q", output) }
}
```

- [ ] **Step 2: Executar RED, implementar el renderer aïllat i executar GREEN**

Run RED/GREEN: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'TestBands'`

`visual_bands.go` defineix els seus markers i backgrounds; no modifica fitxers d’altres estils.

- [ ] **Step 3: Commit**

Commit: `Add turn band renderer`

### Task 5: Slice vertical `cards` incremental

**Files:**
- Create: `internal/ui/visual_cards.go`
- Create: `internal/ui/visual_cards_test.go`
- Modify: `internal/ui/visual_renderer.go` només per al mapping `VisualStyleCards`.

**Interfaces:**
- Consumes: contracte/primitives de Task 2.
- Produces: targeta d’usuari, targeta de Shellia i superfície interna d’execució que s’escriu en streaming.

- [ ] **Step 1: Escriure el RED del lifecycle incremental**

```go
func TestCardsRendererStreamsBeforeCloseAndClosesOnce(t *testing.T) {
	var out bytes.Buffer
	r := NewRenderer(&out, Presentation{Style: configpkg.VisualStyleCards, ANSI: false})
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("first")
	if !strings.Contains(out.String(), "first") { t.Fatal("output was buffered") }
	step.Close()
	beforeTurnClose := out.String()
	turn.Final("done")
	turn.Close()
	afterFirstClose := out.String()
	turn.Close()
	if len(afterFirstClose) <= len(beforeTurnClose) { t.Fatal("turn close did not emit its final border") }
	if out.String() != afterFirstClose { t.Fatal("second Close changed card output") }
}
```

Afegir casos per final normal, blocked, error abans del final i output parcial finalitzat amb `Flush`.

- [ ] **Step 2: Executar el RED**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui -run 'TestCards'`

Expected: FAIL perquè no existeix el renderer o perquè la superfície no té estat incremental.

- [ ] **Step 3: Implementar `visual_cards.go`**

L’estat mínim és `open`, `suspended` i `closed`. `OutputLine` escriu immediatament un lateral; `Close` completa exactament una vora inferior; `Suspend` tanca la superfície actual i `Resume` obre una continuació només si el torn segueix actiu. No es guarda una col·lecció d’output.

- [ ] **Step 4: Verificar errors i commit**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/ui ./internal/app -run 'TestCards|TestRunTurn'`

Expected: PASS, inclosos els retorns anticipats coberts pel `defer turnUI.Close()`.

Commit: `Add incremental card renderer`

### Task 6: Streaming, confirmacions i handoff PTY

**Files:**
- Modify: `internal/executor/writers.go`
- Modify: `internal/executor/executor.go`
- Modify: `internal/executor/executor_test.go`
- Modify: `internal/ui/ui_stepbox_test.go`
- Modify: `internal/app/main_loop_test.go`

**Interfaces:**
- Consumes: `runtimeDeps.Turn`, `Turn.BeginStep`, `Turn.Suspend` i `Turn.Resume`.
- Produces: output línia a línia dins la geometria seleccionada i PTY cru fora de la superfície.

- [ ] **Step 1: Estendre els RED de streaming**

```go
func TestPrefixedWriterStreamsCompleteLinesAndFlushesPartial(t *testing.T) {
	var out bytes.Buffer
	turn := newTestTurn(&out, configpkg.VisualStyleCards, false)
	box := turn.BeginStep(testConfig(), 1, 1, testPlan())
	w := &prefixedWriter{box: box}
	_, _ = w.Write([]byte("one\ntwo"))
	if !strings.Contains(out.String(), "one") || strings.Contains(out.String(), "two") {
		t.Fatalf("pre-flush output = %q", out.String())
	}
	if err := w.Flush(); err != nil { t.Fatal(err) }
	if !strings.Contains(out.String(), "two") { t.Fatalf("post-flush output = %q", out.String()) }
}
```

Repetir amb UTF-8 dividit entre writes, stdout+stderr i `ShowSystemOutput=false`.

- [ ] **Step 2: Connectar passos i confirmacions al torn actiu**

`executeCommands`, `skipCommand` i `executeManualCommand` creen el box a través del torn. `prefixedWriter` continua classificant només fronteres de línia; no interpreta output. Confirmació, edició, completed, timeout i exit code continuen usant els mètodes existents de `stepBox`.

- [ ] **Step 3: Suspendre/reprendre al voltant del PTY**

Immediatament abans d’`executeInteractiveCommand`, fer `deps.Turn.Suspend()`; usar `defer deps.Turn.Resume()` després del retorn. El byte stream continua sent `io.MultiWriter(deps.Stdout, stdoutCapture)` sense prefix, filtratge ANSI ni wrapping.

- [ ] **Step 4: Verificar matriu executor**

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/executor -run 'TestPrefixedWriter|TestExecute|TestPrompt|TestSkip|TestShowCompleted'`

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run 'TestRunTurn|TestRunInteractive'`

Expected: PASS; cap test necessita substituir globals de procés.

- [ ] **Step 5: Smoke PTY manual i commit**

Build: `go build -o /tmp/shellia-visual-styles ./cmd/shellia`

Amb `style = "cards"`, executar una ordre PTY curta mitjançant `/shell`, comprovar que el procés controla el terminal sense vores i que Shellia reprèn una targeta després de sortir.

Commit: `Preserve streaming and PTY handoff across renderers`

### Task 7: Integració, documentació i acceptació final

**Files:**
- Modify: `internal/app/config_test.go`
- Modify: `internal/app/main_loop_test.go`
- Modify: `README.md`
- Modify: només els nous fitxers de renderer si les proves detecten drift respecte la referència.

**Interfaces:**
- Consumes: feature completa.
- Produces: prova visible i documentació de l’usuari.

- [ ] **Step 1: Afegir la matriu d’integració**

Crear una prova table-driven amb quatre estils × ANSI on/off i casos separats no-TTY/`TERM=dumb`. Les assertions comproven:

```go
for _, style := range []configpkg.VisualStyle{
	configpkg.VisualStylePlain,
	configpkg.VisualStyleGuide,
	configpkg.VisualStyleBands,
	configpkg.VisualStyleCards,
} {
	// interactive: user -> plan -> step -> output -> final
	// one-shot: header -> plan -> step -> final
	// no-color: zero "\033[" i geometria pròpia excepte fallback no-TTY
}
```

Usar els mateixos blocs semàntics que la referència visual, sense copiar HTML ni provar píxels.

- [ ] **Step 2: Actualitzar README i plantilla**

Documentar:

```toml
[ui]
style = "plain" # plain | guide | bands | cards
```

Explicar que `--no-color` conserva l’estructura, mentre pipes/redireccions i `TERM=dumb` produeixen `plain` sense ANSI. No prometre configuració de colors.

- [ ] **Step 3: Executar verificació final**

Run: `gofmt -w` només sobre els fitxers Go modificats.

Run: `env GOCACHE=/tmp/go-build go test -count=1 ./...`

Run: `go build -o /tmp/shellia-visual-styles ./cmd/shellia`

Run: `git diff --check`

Renderitzar el transcript canònic a amplades 120, 80 i 48 per als quatre estils, amb i sense ANSI. Comparar jerarquia, niament, densitat i propietat de superfícies amb `terminal-visual-styles-reference.html`; radi, ombres i chrome web no formen part de l’acceptació TUI.

- [ ] **Step 4: Revisar aïllament físic**

Comprovar que:

- cada `visual_<style>.go` només implementa aquell estil;
- cap fitxer d’estil referencia constructors o tipus concrets d’un altre estil;
- l’únic mapping de `VisualStyle` a implementació és `NewRenderer`;
- `app` i `executor` només coneixen `Renderer`, `Turn`, `Presentation` i `StepBox`;
- no hi ha switches d’estil al workflow, executor, prompts o safety.

- [ ] **Step 5: Commit final**

Commit: `Document configurable terminal visual styles`

## Checkpoints

1. Després de Task 2: revisió byte-for-byte de `plain`; no continuar si hi ha drift no aprovat.
2. Després de Tasks 3–4: revisió de jerarquia `guide`/`bands` a 80 i 48 columnes.
3. Després de Task 5: revisió específica que `cards` mostra output abans de `Close` i tanca en tots els outcomes.
4. Després de Task 6: smoke PTY real abans de donar per bona la matriu automatitzada.

## Stop Gates

Aturar i demanar decisió si:

1. preservar `plain` requereix canviar text, ordre o política de confirmació;
2. `cards` només es pot tancar bufferitzant tot l’output;
3. el procés PTY necessita qualsevol transformació de bytes;
4. un renderer necessita importar o modificar un altre renderer;
5. apareixen condicionals d’estil fora de `NewRenderer` o del fitxer propietari;
6. cal canviar planner, safety, traces de decisió o memòria de sessió;
7. una modificació local de l’usuari solapa els mateixos hunks i no es pot preservar.

## Handoff

Quan s’executi aquest pla, usar `implement-project-feature` amb RED/GREEN per slice. L’acceptació acaba quan passen les proves, el build, la comparació amb la referència visual, el smoke PTY i la revisió d’aïllament; no s’afegeixen refinaments visuals o infraestructura fora d’aquest contracte.

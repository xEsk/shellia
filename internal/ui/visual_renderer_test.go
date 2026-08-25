package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xEsk/shellia/internal/core"

	configpkg "github.com/xEsk/shellia/internal/config"
)

func testConfig() ViewOptions {
	return ViewOptions{
		Model:            "gpt-5.4-mini",
		AskConfirmPlan:   true,
		ShowCommandPopup: true,
		VisualStyle:      configpkg.VisualStyleGuide,
		IncludeCWD:       true,
		IncludeUser:      true,
		IncludeOS:        true,
		IncludeShell:     true,
	}
}

func testPlan() core.CommandPlan {
	return core.CommandPlan{Command: "df -h /", Purpose: "Mostrar l'espai lliure.", Risk: "safe"}
}

func newTestTurn(out io.Writer, style configpkg.VisualStyle, ansi bool) *Turn {
	r := NewRenderer(out, Presentation{Style: style, ANSI: ansi})
	return r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
}

type testRendererFactory func(io.Writer, bool) rendererImpl

type recordingRenderer struct {
	userTurns int
}

func (renderer *recordingRenderer) userTurn(core.InteractiveMode, string) {
	renderer.userTurns++
}

func (renderer *recordingRenderer) beginShelliaTurn(ViewOptions, core.ContextInfo) turnImpl {
	return nil
}

func renderConversationFixture(t *testing.T, factory testRendererFactory, ansi bool) string {
	t.Helper()
	var out bytes.Buffer
	r := &Renderer{impl: factory(&out, ansi), ansi: ansi}
	r.UserTurn(core.InteractiveModeAI, "quant d'espai queda al disc?")
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
	turn.Plan("Cal consultar l'espai disponible.", []core.CommandPlan{testPlan()}, false)
	step := turn.BeginStep(1, 1, testPlan())
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
		if next < 0 {
			t.Fatalf("output lacks ordered value %q:\n%s", value, output)
		}
		position += next + len(value)
	}
}

func TestNewRendererSelectsPlain(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out, Presentation{Style: configpkg.VisualStylePlain, ANSI: false})
	if renderer == nil {
		t.Fatal("NewRenderer(plain) = nil")
	}
}

func TestNewRendererSelectsEveryVisualStyle(t *testing.T) {
	tests := []struct {
		name  string
		style configpkg.VisualStyle
		want  string
	}{
		{name: "plain", style: configpkg.VisualStylePlain, want: "you › selected"},
		{name: "guide", style: configpkg.VisualStyleGuide, want: "┃ you"},
		{name: "bands", style: configpkg.VisualStyleBands, want: "▌  you"},
		{name: "cards", style: configpkg.VisualStyleCards, want: "╭─ you"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			renderer := NewRenderer(&out, Presentation{Style: tt.style, ANSI: false})
			renderer.UserTurn(core.InteractiveModeAI, "selected")
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("NewRenderer(%q) output lacks %q: %q", tt.style, tt.want, out.String())
			}
		})
	}
}

// TestEveryVisualStyleKeepsMarkdownTablesInsideItsAnswerSurface checks tables
// remain table-shaped without adding an outer box inside any visual theme.
func TestEveryVisualStyleKeepsMarkdownTablesInsideItsAnswerSurface(t *testing.T) {
	message := "| Directori | Mida |\n| --- | ---: |\n| /var/www | 7,9G |\n| /tmp | 195M |"
	styles := []configpkg.VisualStyle{
		configpkg.VisualStylePlain,
		configpkg.VisualStyleGuide,
		configpkg.VisualStyleBands,
		configpkg.VisualStyleCards,
	}

	for _, visualStyle := range styles {
		t.Run(string(visualStyle), func(t *testing.T) {
			var out bytes.Buffer
			turn := NewRenderer(&out, Presentation{Style: visualStyle, ANSI: false}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
			turn.Final(message)
			turn.Close()

			output := out.String()
			for _, required := range []string{"Directori", "/var/www", "│", "─"} {
				if !strings.Contains(output, required) {
					t.Fatalf("%s output missing table content %q:\n%s", visualStyle, required, output)
				}
			}
			for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
				if width := visibleWidth(line); width > 80 {
					t.Fatalf("%s output line width = %d, want <= 80: %q", visualStyle, width, line)
				}
			}
		})
	}
}

func TestEveryVisualStyleUsesConfiguredPromptUser(t *testing.T) {
	styles := []configpkg.VisualStyle{
		configpkg.VisualStylePlain,
		configpkg.VisualStyleGuide,
		configpkg.VisualStyleBands,
		configpkg.VisualStyleCards,
	}

	for _, visualStyle := range styles {
		t.Run(string(visualStyle), func(t *testing.T) {
			renderer := NewRenderer(io.Discard, Presentation{Style: visualStyle, User: "xesc"})
			if got, want := renderer.interactivePromptPrefix(false, core.InteractiveModeAI), "xesc › "; got != want {
				t.Fatalf("interactive prompt = %q, want %q", got, want)
			}
		})
	}
}

func TestNewRendererFallsBackToPlain(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out, Presentation{Style: configpkg.VisualStyle("unknown"), ANSI: false})
	renderer.UserTurn(core.InteractiveModeAI, "fallback")
	if got, want := out.String(), "you › fallback\r\n"; got != want {
		t.Fatalf("unknown style output = %q, want plain %q", got, want)
	}
}

func TestRendererFacadesAreNilSafe(t *testing.T) {
	var renderer *Renderer
	renderer.UserTurn(core.InteractiveModeAI, "ignored")
	turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{})
	turn.Plan("ignored", nil, false)
	if step := turn.BeginStep(1, 1, testPlan()); step != nil {
		t.Fatalf("nil renderer BeginStep() = %#v, want nil", step)
	}
	turn.Final("ignored")
	turn.Suspend()
	turn.Resume()
	turn.Close()
}

// TestTurnUsesOwnedViewOptions keeps turn rendering independent from a full runtime config.
func TestTurnUsesOwnedViewOptions(t *testing.T) {
	var out bytes.Buffer
	options := ViewOptions{Verbose: true}
	turn := NewRenderer(&out, Presentation{Style: configpkg.VisualStylePlain}).BeginShelliaTurn(options, core.ContextInfo{})
	turn.Plan("Inspect disk space.", []core.CommandPlan{testPlan()}, false)
	step := turn.BeginStep(1, 1, testPlan())
	step.Close()

	if got := out.String(); !strings.Contains(got, "risk") {
		t.Fatalf("turn output = %q, want verbose risk details from its owned options", got)
	}
}

func TestSubmittedShellPromptBypassesConversationRenderer(t *testing.T) {
	var out bytes.Buffer
	recording := &recordingRenderer{}
	renderer := &Renderer{impl: recording}
	renderSubmittedPrompt(&out, false, "shell › ", []rune("pwd"), core.InteractiveModeShell, renderer)
	if recording.userTurns != 0 {
		t.Fatalf("shell prompt renderer calls = %d, want 0", recording.userTurns)
	}
	if got, want := out.String(), "shell › pwd\r\n"; got != want {
		t.Fatalf("shell prompt output = %q, want %q", got, want)
	}
}

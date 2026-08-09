package ui

import (
	"bytes"
	"io"
	"shellia/internal/core"
	"strings"
	"testing"

	configpkg "shellia/internal/config"
)

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

type testRendererFactory func(io.Writer, bool) rendererImpl

type recordingRenderer struct {
	userTurns int
}

func (renderer *recordingRenderer) userTurn(core.InteractiveMode, string) {
	renderer.userTurns++
}

func (renderer *recordingRenderer) beginShelliaTurn(configpkg.Config, core.ContextInfo) turnImpl {
	return nil
}

func renderConversationFixture(t *testing.T, factory testRendererFactory, ansi bool) string {
	t.Helper()
	var out bytes.Buffer
	r := &Renderer{impl: factory(&out, ansi), ansi: ansi}
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
	turn.Plan(testConfig(), "ignored", nil, false)
	if step := turn.BeginStep(testConfig(), 1, 1, testPlan()); step != nil {
		t.Fatalf("nil renderer BeginStep() = %#v, want nil", step)
	}
	turn.Final("ignored")
	turn.Suspend()
	turn.Resume()
	turn.Close()
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

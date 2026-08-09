package app

import (
	"os"
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
)

func TestEffectivePresentation(t *testing.T) {
	tests := []struct {
		name      string
		style     configpkg.VisualStyle
		noColor   bool
		tty       bool
		term      string
		wantStyle configpkg.VisualStyle
		wantANSI  bool
	}{
		{name: "tty plain", style: configpkg.VisualStylePlain, tty: true, term: "xterm-256color", wantStyle: configpkg.VisualStylePlain, wantANSI: true},
		{name: "tty guide", style: configpkg.VisualStyleGuide, tty: true, term: "xterm-256color", wantStyle: configpkg.VisualStyleGuide, wantANSI: true},
		{name: "tty bands", style: configpkg.VisualStyleBands, tty: true, term: "xterm-256color", wantStyle: configpkg.VisualStyleBands, wantANSI: true},
		{name: "tty cards", style: configpkg.VisualStyleCards, tty: true, term: "xterm-256color", wantStyle: configpkg.VisualStyleCards, wantANSI: true},
		{name: "no color plain", style: configpkg.VisualStylePlain, noColor: true, tty: true, term: "xterm", wantStyle: configpkg.VisualStylePlain, wantANSI: false},
		{name: "no color guide", style: configpkg.VisualStyleGuide, noColor: true, tty: true, term: "xterm", wantStyle: configpkg.VisualStyleGuide, wantANSI: false},
		{name: "no color bands", style: configpkg.VisualStyleBands, noColor: true, tty: true, term: "xterm", wantStyle: configpkg.VisualStyleBands, wantANSI: false},
		{name: "no color cards", style: configpkg.VisualStyleCards, noColor: true, tty: true, term: "xterm", wantStyle: configpkg.VisualStyleCards, wantANSI: false},
		{name: "pipe forces plain", style: configpkg.VisualStyleCards, tty: false, term: "xterm", wantStyle: configpkg.VisualStylePlain, wantANSI: false},
		{name: "dumb terminal forces plain", style: configpkg.VisualStyleBands, tty: true, term: "dumb", wantStyle: configpkg.VisualStylePlain, wantANSI: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", tt.term)
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()

			cfg := configpkg.DefaultConfig()
			cfg.VisualStyle = tt.style
			cfg.NoColor = tt.noColor
			deps := runtimeDeps{
				Stdout:           stdout,
				StdoutIsTerminal: func(*os.File) bool { return tt.tty },
			}

			got := effectivePresentation(cfg, deps)
			if got.Style != tt.wantStyle || got.ANSI != tt.wantANSI {
				t.Fatalf("effectivePresentation() = %#v, want style=%q ANSI=%t", got, tt.wantStyle, tt.wantANSI)
			}
		})
	}
}

func TestPromptPresentationUserHonorsConfiguredIdentity(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	if got, want := promptPresentationUser(cfg, "xesc"), "xesc"; got != want {
		t.Fatalf("user identity label = %q, want %q", got, want)
	}

	cfg.PromptIdentity = configpkg.PromptIdentityYou
	if got := promptPresentationUser(cfg, "xesc"); got != "" {
		t.Fatalf("you identity label = %q, want empty renderer fallback", got)
	}
}

package ui

import (
	"bytes"
	"shellia/internal/core"
	"testing"

	configpkg "shellia/internal/config"
)

func TestPlainRendererPreservesConversationBytes(t *testing.T) {
	got := renderConversationFixture(t, newPlainRenderer, false)
	want := "you › quant d'espai queda al disc?\r\n" + `
Shellia · dev
/Users/Xesc/Documents/Scripts

plan
  Cal consultar l'espai disponible.

steps
  1. Mostrar l'espai lliure.
  run › df -h /

────────────────────────────────────────────────────────────────────────────────

step 1/1

run › df -h /

• Mostrar l'espai lliure.
• system output

  419Gi available

Shellia
  Queden 419Gi lliures al disc arrel (/).
────────────────────────────────────────────────────────────────────────────────
`
	if got != want {
		t.Fatalf("plain conversation bytes changed:\n got: %q\nwant: %q", got, want)
	}
}

func TestPlainRendererPreservesSubmittedPromptWhitespace(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out, Presentation{Style: configpkg.VisualStylePlain})
	renderSubmittedPrompt(&out, false, "you › ", []rune("  spaced  "), core.InteractiveModeAI, renderer)
	if got, want := out.String(), "you ›   spaced  \r\n"; got != want {
		t.Fatalf("submitted prompt = %q, want %q", got, want)
	}
}

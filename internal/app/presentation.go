package app

import (
	"os"
	"strings"

	configpkg "github.com/xEsk/shellia/internal/config"
)

// effectivePresentationState holds the terminal structure and ANSI capability for one Shellia run.
type effectivePresentationState struct {
	Style configpkg.VisualStyle
	ANSI  bool
}

// effectivePresentation resolves the configured visual style for the injected output stream.
func effectivePresentation(cfg config, deps runtimeDeps) effectivePresentationState {
	if deps.Stdout == nil || deps.StdoutIsTerminal == nil ||
		!deps.StdoutIsTerminal(deps.Stdout) ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return effectivePresentationState{Style: configpkg.VisualStylePlain}
	}

	return effectivePresentationState{
		Style: cfg.VisualStyle,
		ANSI:  !cfg.NoColor,
	}
}

func promptPresentationUser(cfg config, user string) string {
	if cfg.PromptIdentity == configpkg.PromptIdentityYou {
		return ""
	}
	return strings.TrimSpace(user)
}

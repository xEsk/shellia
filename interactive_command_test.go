package main

import "testing"

// TestParseInteractiveCommandSlashCommands checks the supported interactive slash commands.
func TestParseInteractiveCommandSlashCommands(t *testing.T) {
	tests := map[string]interactiveCommand{
		"/shell":    interactiveCommandShell,
		" /SHELL  ": interactiveCommandShell,
		"/ai":       interactiveCommandAI,
		"/mode":     interactiveCommandMode,
		"/context":  interactiveCommandContext,
		"/clear":    interactiveCommandClear,
		"/exit":     interactiveCommandExit,
		"/quit":     interactiveCommandExit,
		"exit":      interactiveCommandExit,
	}

	for input, want := range tests {
		if got := parseInteractiveCommand(input); got != want {
			t.Fatalf("parseInteractiveCommand(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestParseInteractiveCommandRejectsLegacyColonCommands checks that old colon commands are no longer control commands.
func TestParseInteractiveCommandRejectsLegacyColonCommands(t *testing.T) {
	for _, input := range []string{":shell", ":ai", ":mode", "clear", "context", "quit"} {
		if got := parseInteractiveCommand(input); got != interactiveCommandNone {
			t.Fatalf("parseInteractiveCommand(%q) = %q, want no command", input, got)
		}
	}
}

// TestParseInteractiveCommandUnknownSlash checks typos do not fall through to the model.
func TestParseInteractiveCommandUnknownSlash(t *testing.T) {
	if got := parseInteractiveCommand("/shel"); got != interactiveCommandUnknown {
		t.Fatalf("parseInteractiveCommand(/shel) = %q, want %q", got, interactiveCommandUnknown)
	}
}

// TestParseInteractiveCommandAllowsAbsolutePaths checks shell paths can still be executed.
func TestParseInteractiveCommandAllowsAbsolutePaths(t *testing.T) {
	for _, input := range []string{"/usr/bin/env", "/Users/me/script.sh --flag"} {
		if got := parseInteractiveCommand(input); got != interactiveCommandNone {
			t.Fatalf("parseInteractiveCommand(%q) = %q, want no command", input, got)
		}
	}
}

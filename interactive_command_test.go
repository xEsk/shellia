package main

import "testing"

// TestParseInteractiveCommandSlashCommands checks the supported interactive slash commands.
func TestParseInteractiveCommandSlashCommands(t *testing.T) {
	tests := map[string]interactiveCommand{
		"/shell":     interactiveCommandShell,
		" /SHELL  ":  interactiveCommandShell,
		"/plan":      interactiveCommandPlan,
		"/ai":        interactiveCommandAI,
		"/mode":      interactiveCommandMode,
		"/model":     interactiveCommandModel,
		"/model mlx": interactiveCommandModel,
		"/context":   interactiveCommandContext,
		"/clear":     interactiveCommandClear,
		"/exit":      interactiveCommandExit,
		"/quit":      interactiveCommandExit,
		"exit":       interactiveCommandExit,
	}

	for input, want := range tests {
		if got := parseInteractiveCommand(input); got != want {
			t.Fatalf("parseInteractiveCommand(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestParsePlanInstructionExtractsPrompt checks /plan keeps the user request as the planned task.
func TestParsePlanInstructionExtractsPrompt(t *testing.T) {
	ok, instruction := parsePlanInstruction(" /plan create a backup ")
	if !ok {
		t.Fatalf("parsePlanInstruction() ok = false, want true")
	}
	if instruction != "create a backup" {
		t.Fatalf("parsePlanInstruction() instruction = %q, want %q", instruction, "create a backup")
	}
}

// TestParsePlanInstructionIgnoresOtherSlashCommands checks only /plan enables plan-only mode.
func TestParsePlanInstructionIgnoresOtherSlashCommands(t *testing.T) {
	ok, instruction := parsePlanInstruction("/shell")
	if ok || instruction != "" {
		t.Fatalf("parsePlanInstruction(/shell) = %t, %q; want false, empty", ok, instruction)
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

// TestMatchingInteractiveSlashCommandsFiltersByPrefix checks the prompt menu candidates.
func TestMatchingInteractiveSlashCommandsFiltersByPrefix(t *testing.T) {
	got := matchingInteractiveSlashCommands("/co")
	if len(got) != 1 || got[0].Input != "/context" {
		t.Fatalf("matchingInteractiveSlashCommands(/co) = %#v, want /context", got)
	}

	all := matchingInteractiveSlashCommands("/")
	if len(all) != len(interactiveSlashCommands) {
		t.Fatalf("matchingInteractiveSlashCommands(/) returned %d commands, want %d", len(all), len(interactiveSlashCommands))
	}
}

// TestMatchingInteractiveSlashCommandsIgnoresNonControlPaths avoids noisy menus for absolute paths.
func TestMatchingInteractiveSlashCommandsIgnoresNonControlPaths(t *testing.T) {
	for _, input := range []string{"/usr/bin/env", "/Users/me/script.sh", " /shell", "/shell now"} {
		if got := matchingInteractiveSlashCommands(input); len(got) != 0 {
			t.Fatalf("matchingInteractiveSlashCommands(%q) = %#v, want no suggestions", input, got)
		}
	}
}

// TestCompleteInteractiveSlashCommandUsesFirstMatch checks the selected command for Tab completion.
func TestCompleteInteractiveSlashCommandUsesFirstMatch(t *testing.T) {
	got, ok := completeInteractiveSlashCommand("/s")
	if !ok || got != "/shell" {
		t.Fatalf("completeInteractiveSlashCommand(/s) = %q, %t; want /shell, true", got, ok)
	}

	if got, ok := completeInteractiveSlashCommand("/usr/bin/env"); ok {
		t.Fatalf("completeInteractiveSlashCommand(path) = %q, true; want no completion", got)
	}
}

// TestParseModelCommandName extracts the selected model profile from /model.
func TestParseModelCommandName(t *testing.T) {
	if got := parseModelCommandName(" /model mlx "); got != "mlx" {
		t.Fatalf("parseModelCommandName() = %q, want mlx", got)
	}
	if got := parseModelCommandName("/model"); got != "" {
		t.Fatalf("parseModelCommandName(/model) = %q, want empty", got)
	}
}

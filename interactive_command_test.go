package main

import "testing"

// TestParseInteractiveCommandSlashCommands checks the supported interactive slash commands.
func TestParseInteractiveCommandSlashCommands(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  interactiveCommand
	}{
		{name: "shell", input: "/shell", want: interactiveCommandShell},
		{name: "shell uppercase spaced", input: " /SHELL  ", want: interactiveCommandShell},
		{name: "plan", input: "/plan", want: interactiveCommandPlan},
		{name: "retry", input: "/retry", want: interactiveCommandRetry},
		{name: "new", input: "/new", want: interactiveCommandNew},
		{name: "ai", input: "/ai", want: interactiveCommandAI},
		{name: "mode", input: "/mode", want: interactiveCommandMode},
		{name: "model", input: "/model", want: interactiveCommandModel},
		{name: "model argument", input: "/model mlx", want: interactiveCommandModel},
		{name: "context", input: "/context", want: interactiveCommandContext},
		{name: "clear", input: "/clear", want: interactiveCommandClear},
		{name: "exit slash", input: "/exit", want: interactiveCommandExit},
		{name: "quit slash", input: "/quit", want: interactiveCommandExit},
		{name: "exit bare", input: "exit", want: interactiveCommandExit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInteractiveCommand(tt.input); got != tt.want {
				t.Fatalf("parseInteractiveCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
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
	cases := []struct {
		name  string
		input string
	}{
		{name: "colon shell", input: ":shell"},
		{name: "colon ai", input: ":ai"},
		{name: "colon mode", input: ":mode"},
		{name: "clear bare", input: "clear"},
		{name: "context bare", input: "context"},
		{name: "quit bare", input: "quit"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInteractiveCommand(tt.input); got != interactiveCommandNone {
				t.Fatalf("parseInteractiveCommand(%q) = %q, want no command", tt.input, got)
			}
		})
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
	cases := []struct {
		name  string
		input string
	}{
		{name: "binary path", input: "/usr/bin/env"},
		{name: "script path with flag", input: "/Users/me/script.sh --flag"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInteractiveCommand(tt.input); got != interactiveCommandNone {
				t.Fatalf("parseInteractiveCommand(%q) = %q, want no command", tt.input, got)
			}
		})
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
	cases := []struct {
		name  string
		input string
	}{
		{name: "binary path", input: "/usr/bin/env"},
		{name: "script path", input: "/Users/me/script.sh"},
		{name: "spaced shell command", input: " /shell"},
		{name: "shell command with argument", input: "/shell now"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchingInteractiveSlashCommands(tt.input); len(got) != 0 {
				t.Fatalf("matchingInteractiveSlashCommands(%q) = %#v, want no suggestions", tt.input, got)
			}
		})
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

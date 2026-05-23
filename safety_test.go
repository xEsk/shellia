package main

import "testing"

// TestClassifyCommandRequiresConfirmationForCompoundSafeRoots checks safe roots cannot hide extra commands.
func TestClassifyCommandRequiresConfirmationForCompoundSafeRoots(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{name: "background operator", command: "echo ok & rm -rf tmp"},
		{name: "and operator", command: "echo ok && rm -rf tmp"},
		{name: "newline separator", command: "echo ok\nrm -rf tmp"},
		{name: "carriage return separator", command: "echo ok\rrm -rf tmp"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification == classificationSafe || !got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", tt.command, got)
			}
		})
	}
}

// TestHasShellOperatorsIgnoresQuotedAndEscapedSeparators checks ordinary shell text is not over-classified.
func TestHasShellOperatorsIgnoresQuotedAndEscapedSeparators(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{name: "double quoted ampersand", command: `echo "a & b"`},
		{name: "single quoted ampersand", command: `echo 'a & b'`},
		{name: "escaped ampersand", command: `echo a\&b`},
		{name: "quoted newline text", command: `echo "first\nsecond"`},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if hasShellOperators(tt.command) {
				t.Fatalf("hasShellOperators(%q) = true, want false", tt.command)
			}
		})
	}
}

// TestClassifyCommandAllowsOnlyKnownReadOnlyGitSubcommands checks git does not fall through as generically safe.
func TestClassifyCommandAllowsOnlyKnownReadOnlyGitSubcommands(t *testing.T) {
	safeCommands := []struct {
		name    string
		command string
	}{
		{name: "status", command: "git status"},
		{name: "log", command: "git log"},
		{name: "show", command: "git show HEAD"},
		{name: "diff", command: "git diff"},
		{name: "rev parse", command: "git rev-parse --show-toplevel"},
		{name: "remote verbose", command: "git remote -v"},
	}

	for _, tt := range safeCommands {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification != classificationSafe || got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want safe without confirmation", tt.command, got)
			}
		})
	}

	riskyCommands := []struct {
		name    string
		command string
	}{
		{name: "config", command: "git config user.name Shellia"},
		{name: "fetch", command: "git fetch"},
		{name: "branch", command: "git branch"},
		{name: "worktree", command: "git worktree list"},
	}

	for _, tt := range riskyCommands {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification == classificationSafe || !got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", tt.command, got)
			}
		})
	}
}

// TestClassifyCommandRequiresConfirmationForMutatingFind checks find actions cannot auto-run under yes-safe.
func TestClassifyCommandRequiresConfirmationForMutatingFind(t *testing.T) {
	riskyCommands := []struct {
		name    string
		command string
	}{
		{name: "delete", command: "find . -delete"},
		{name: "exec rm", command: "find . -name '*.tmp' -exec rm {} \\;"},
		{name: "execdir chmod", command: "find . -execdir chmod 644 {} \\;"},
		{name: "ok rm", command: "find . -ok rm {} \\;"},
		{name: "okdir rm", command: "find . -okdir rm {} \\;"},
	}

	for _, tt := range riskyCommands {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification == classificationSafe || !got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", tt.command, got)
			}
		})
	}
}

// TestClassifyCommandAllowsReadOnlyFind checks common read-only find commands stay safe.
func TestClassifyCommandAllowsReadOnlyFind(t *testing.T) {
	safeCommands := []struct {
		name    string
		command string
	}{
		{name: "name", command: "find . -name '*.go'"},
		{name: "type maxdepth", command: "find . -type f -maxdepth 2"},
		{name: "mtime print", command: "find . -mtime -1 -print"},
	}

	for _, tt := range safeCommands {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification != classificationSafe || got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want safe without confirmation", tt.command, got)
			}
		})
	}
}

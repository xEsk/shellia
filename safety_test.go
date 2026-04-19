package main

import "testing"

// TestClassifyCommandRequiresConfirmationForCompoundSafeRoots checks safe roots cannot hide extra commands.
func TestClassifyCommandRequiresConfirmationForCompoundSafeRoots(t *testing.T) {
	cases := []string{
		"echo ok & rm -rf tmp",
		"echo ok && rm -rf tmp",
		"echo ok\nrm -rf tmp",
		"echo ok\rrm -rf tmp",
	}

	for _, command := range cases {
		got := classifyCommand(command)
		if got.Classification == classificationSafe || !got.RequiresConfirmation {
			t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", command, got)
		}
	}
}

// TestHasShellOperatorsIgnoresQuotedAndEscapedSeparators checks ordinary shell text is not over-classified.
func TestHasShellOperatorsIgnoresQuotedAndEscapedSeparators(t *testing.T) {
	cases := []string{
		`echo "a & b"`,
		`echo 'a & b'`,
		`echo a\&b`,
		`echo "first\nsecond"`,
	}

	for _, command := range cases {
		if hasShellOperators(command) {
			t.Fatalf("hasShellOperators(%q) = true, want false", command)
		}
	}
}

// TestClassifyCommandAllowsOnlyKnownReadOnlyGitSubcommands checks git does not fall through as generically safe.
func TestClassifyCommandAllowsOnlyKnownReadOnlyGitSubcommands(t *testing.T) {
	safeCommands := []string{
		"git status",
		"git log",
		"git show HEAD",
		"git diff",
		"git rev-parse --show-toplevel",
		"git remote -v",
	}

	for _, command := range safeCommands {
		got := classifyCommand(command)
		if got.Classification != classificationSafe || got.RequiresConfirmation {
			t.Fatalf("classifyCommand(%q) = %#v, want safe without confirmation", command, got)
		}
	}

	riskyCommands := []string{
		"git config user.name Shellia",
		"git fetch",
		"git branch",
		"git worktree list",
	}

	for _, command := range riskyCommands {
		got := classifyCommand(command)
		if got.Classification == classificationSafe || !got.RequiresConfirmation {
			t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", command, got)
		}
	}
}

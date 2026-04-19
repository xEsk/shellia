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

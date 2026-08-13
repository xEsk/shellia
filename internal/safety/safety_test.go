package safety

import "testing"

// TestClassifyCommandFindsWrappedAndQuotedDangerousRoots checks wrappers and
// cosmetic quoting cannot hide the executable from local safety policy.
func TestClassifyCommandFindsWrappedAndQuotedDangerousRoots(t *testing.T) {
	commands := []string{
		"env LC_ALL=C rm -rf tmp",
		"env -P /bin rm -rf tmp",
		`env -S 'rm -rf tmp'`,
		`env -S '-i rm -rf tmp'`,
		`env -S '"/usr/bin/rm" -rf tmp'`,
		`"/bin/rm" -rf tmp`,
	}

	for _, command := range commands {
		got := classifyCommand(command)
		if got.Classification != classificationDangerous || got.Risk != riskHigh || !got.RequiresConfirmation {
			t.Errorf("classifyCommand(%q) = %#v, want dangerous high-risk confirmation", command, got)
		}
	}
}

// TestClassifyCommandRejectsExecutableSubstitutions catches safe-root bypasses through command substitution syntax.
func TestClassifyCommandRejectsExecutableSubstitutions(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    commandSafety
	}{
		{
			name:    "dollar substitution outside quotes",
			command: `echo $(whoami)`,
			want:    commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true},
		},
		{
			name:    "backtick substitution outside quotes",
			command: "echo `whoami`",
			want:    commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true},
		},
		{
			name:    "dollar substitution inside double quotes",
			command: `echo "$(whoami)"`,
			want:    commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true},
		},
		{
			name:    "backtick substitution inside double quotes",
			command: "echo \"`whoami`\"",
			want:    commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true},
		},
		{
			name:    "dangerous dollar substitution",
			command: `echo "$(rm -rf /)"`,
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "dangerous backtick substitution",
			command: "echo \"`rm -rf /`\"",
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "nested dollar substitutions",
			command: `echo "$(printf '%s' "$(whoami)")"`,
			want:    commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCommand(tt.command); got != tt.want {
				t.Fatalf("classifyCommand(%q) = %#v, want %#v", tt.command, got, tt.want)
			}
		})
	}
}

// TestClassifyCommandFindsObscuredSubstitutionRoots catches dangerous roots hidden by shell word syntax.
func TestClassifyCommandFindsObscuredSubstitutionRoots(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    commandSafety
	}{
		{
			name:    "fully quoted dollar substitution root",
			command: `echo "$("rm" -rf /)"`,
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "partially quoted dollar substitution root",
			command: `echo "$(r"m" -rf /)"`,
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "fully quoted backtick substitution root",
			command: "echo \x60\"rm\" -rf /\x60",
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "partially quoted backtick substitution root",
			command: "echo \x60r\"m\" -rf /\x60",
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "dangerous escaped nested backticks",
			command: "echo \x60echo \\\x60rm -rf /\\\x60\x60",
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
		{
			name:    "assignment before dangerous root",
			command: `echo "$(VAR=value rm -rf /)"`,
			want:    commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCommand(tt.command); got != tt.want {
				t.Fatalf("classifyCommand(%q) = %#v, want %#v", tt.command, got, tt.want)
			}
		})
	}
}

// TestClassifyCommandPreservesLiteralSubstitutionText catches over-classification of non-executable markers.
func TestClassifyCommandPreservesLiteralSubstitutionText(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    commandSafety
	}{
		{
			name:    "escaped dollar substitution marker",
			command: `echo "\$(whoami)"`,
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
		{
			name:    "escaped backtick substitution markers",
			command: "echo \"\\`whoami\\`\"",
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
		{
			name:    "single quoted dollar substitution",
			command: `echo '$(whoami)'`,
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
		{
			name:    "single quoted backtick substitution",
			command: "echo '`whoami`'",
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
		{
			name:    "double quoted redirect text",
			command: `echo "a > b"`,
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
		{
			name:    "existing read only command",
			command: `ls -la`,
			want:    commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCommand(tt.command); got != tt.want {
				t.Fatalf("classifyCommand(%q) = %#v, want %#v", tt.command, got, tt.want)
			}
		})
	}
}

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

// TestClassifyCommandEscalatesDangerousCompoundRoots checks hidden dangerous roots stay high risk.
func TestClassifyCommandEscalatesDangerousCompoundRoots(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{name: "and rm", command: "echo ok && rm -rf tmp"},
		{name: "pipeline sudo", command: "cat file | sudo tee /etc/hosts"},
		{name: "semicolon chmod", command: "echo ok; chmod 777 script.sh"},
		{name: "background chown", command: "echo ok & chown root file"},
		{name: "subshell rm", command: "(rm -rf tmp)"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification != classificationDangerous || got.Risk != riskHigh || !got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want high-risk confirmation", tt.command, got)
			}
		})
	}
}

// TestClassifyCommandDoesNotEscalateQuotedDangerousWords checks quoted text is not treated as a root.
func TestClassifyCommandDoesNotEscalateQuotedDangerousWords(t *testing.T) {
	got := classifyCommand(`echo "rm -rf tmp" && true`)
	if got.Classification == classificationDangerous || got.Risk == riskHigh {
		t.Fatalf("classifyCommand() = %#v, want compound risky but not dangerous", got)
	}
	if !got.RequiresConfirmation {
		t.Fatalf("classifyCommand() = %#v, want confirmation for compound shell", got)
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
		{name: "quoted parenthesis", command: `echo "(not a subshell)"`},
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
		{name: "remote get url", command: "git remote get-url --all origin"},
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
		{name: "remote remove", command: "git remote remove origin"},
		{name: "remote set url", command: "git remote set-url origin https://example.invalid/repo.git"},
		{name: "diff output", command: "git diff --output=changes.patch"},
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
		{name: "fprint", command: "find . -fprint results.txt"},
		{name: "fprint zero", command: "find . -fprint0 results.bin"},
		{name: "fprintf", command: "find . -fprintf results.txt '%p\\n'"},
		{name: "fls", command: "find . -fls results.txt"},
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

// TestClassifyCommandRequiresConfirmationForRipgrepExecutables checks ripgrep cannot launch helpers under yes-safe.
func TestClassifyCommandRequiresConfirmationForRipgrepExecutables(t *testing.T) {
	riskyCommands := []struct {
		name    string
		command string
	}{
		{name: "pre separate", command: "rg --pre sh marker file"},
		{name: "pre equals", command: "rg --pre=sh marker file"},
		{name: "hostname separate", command: "rg --hostname-bin hostname-helper marker file"},
		{name: "hostname equals", command: "rg --hostname-bin=hostname-helper marker file"},
	}

	for _, tt := range riskyCommands {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCommand(tt.command)
			if got.Classification == classificationSafe || !got.RequiresConfirmation {
				t.Fatalf("classifyCommand(%q) = %#v, want confirmation required", tt.command, got)
			}
		})
	}

	got := classifyCommand("rg --hidden marker internal")
	if got.Classification != classificationSafe || got.RequiresConfirmation {
		t.Fatalf("classifyCommand() = %#v, want ordinary ripgrep search safe without confirmation", got)
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

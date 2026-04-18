package main

import "strings"

// Risk level constants. Used across classifyCommand, higherRisk, riskBadge.
const (
	riskSafe   = "safe"
	riskMedium = "medium"
	riskHigh   = "high"
)

// Classification constants. Used across classifyCommand, classificationBadge, normalizePlan.
const (
	classificationSafe      = "safe"
	classificationRisky     = "risky"
	classificationDangerous = "dangerous"
)

type commandSafety struct {
	Classification       string
	Risk                 string
	RequiresConfirmation bool
}

// classifyCommand applies a conservative local policy to each command.
func classifyCommand(command string) commandSafety {
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true}
	}

	if hasShellOperators(command) {
		return commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true}
	}

	base := tokens[0]

	dangerousRoots := map[string]bool{
		"sudo": true, "su": true, "rm": true, "dd": true, "mkfs": true, "shutdown": true,
		"reboot": true, "halt": true, "poweroff": true, "useradd": true, "adduser": true,
		"usermod": true, "userdel": true, "groupadd": true, "groupdel": true, "passwd": true,
		"chmod": true, "chown": true, "chgrp": true,
	}
	if dangerousRoots[base] {
		return commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true}
	}

	if base == "git" && len(tokens) > 1 {
		// "branch" excluded: `git branch -D` is destructive and subflags can't be checked here.
		safeGit := map[string]bool{
			"status": true, "log": true, "show": true, "diff": true, "rev-parse": true, "remote": true,
		}
		if safeGit[tokens[1]] {
			return commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false}
		}
	}

	if base == "docker" && len(tokens) > 1 {
		if isSafeDockerRead(tokens) {
			return commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false}
		}
	}

	if isUserOrSystemModification(tokens) {
		return commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true}
	}

	if isFilesystemModification(tokens) {
		return commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true}
	}

	safeRoots := map[string]bool{
		"ls": true, "pwd": true, "cat": true, "echo": true, "whoami": true, "id": true,
		"uname": true, "date": true, "git": true, "grep": true, "rg": true, "which": true,
		"whereis": true, "find": true, "head": true, "tail": true, "wc": true, "stat": true,
		"du": true, "df": true, "ps": true, "man": true,
	}
	if safeRoots[base] {
		return commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false}
	}

	return commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true}
}

// isSafeDockerRead reports whether the docker command is a read-only inspection.
func isSafeDockerRead(tokens []string) bool {
	if len(tokens) < 2 || tokens[0] != "docker" {
		return false
	}

	switch tokens[1] {
	case "images", "ps", "version", "info":
		return true
	case "image":
		return len(tokens) > 2 && tokens[2] == "ls"
	case "container":
		return len(tokens) > 2 && tokens[2] == "ls"
	case "inspect":
		return true
	default:
		return false
	}
}

// isFilesystemModification detects potential changes to the filesystem.
func isFilesystemModification(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}

	modifyingRoots := map[string]bool{
		"rm": true, "mv": true, "cp": true, "touch": true, "mkdir": true, "rmdir": true,
		"install": true, "truncate": true, "tee": true, "sed": true, "awk": true, "perl": true,
		"tar": true, "unzip": true, "zip": true, "ln": true, "chmod": true, "chown": true, "chgrp": true,
	}
	if modifyingRoots[tokens[0]] {
		return true
	}

	if tokens[0] == "git" && len(tokens) > 1 {
		dangerousGit := map[string]bool{
			"add": true, "apply": true, "am": true, "checkout": true, "switch": true, "restore": true,
			"pull": true, "merge": true, "rebase": true, "reset": true, "clean": true, "commit": true,
			"push": true, "stash": true, "tag": true, "branch": true,
		}
		return dangerousGit[tokens[1]]
	}

	return false
}

// isUserOrSystemModification detects actions on users, services, or packages.
func isUserOrSystemModification(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}

	// Note: sudo, su, useradd/adduser/usermod/userdel, groupadd/groupdel, passwd are already
	// caught by dangerousRoots above, so they are intentionally absent here to avoid dead overlap.
	systemRoots := map[string]bool{
		"systemctl": true, "service": true, "launchctl": true,
		"apt": true, "apt-get": true, "yum": true, "dnf": true, "apk": true,
		"pacman": true, "brew": true, "pip": true, "pip3": true, "npm": true, "pnpm": true,
		"yarn": true, "docker": true, "kubectl": true, "sysctl": true,
	}

	return systemRoots[tokens[0]]
}

// hasShellOperators detects shell operators outside of quoted strings.
// Prevents false positives like: echo "a > b" or grep 'a|b'.
func hasShellOperators(command string) bool {
	inSingle := false
	inDouble := false
	runes := []rune(command)
	n := len(runes)

	for i := 0; i < n; i++ {
		ch := runes[i]

		if inSingle {
			// Inside single quotes there are no escapes; only ' closes.
			if ch == '\'' {
				inSingle = false
			}
			continue
		}

		if inDouble {
			// Inside double quotes, \ escapes the next character.
			if ch == '\\' {
				i++
				continue
			}
			if ch == '"' {
				inDouble = false
			}
			continue
		}

		// Outside quotes.
		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '\\':
			i++ // skip escaped character
		case ';', '`':
			return true
		case '|':
			return true // both | and ||
		case '&':
			if i+1 < n && runes[i+1] == '&' {
				return true
			}
		case '>':
			return true // both > and >>
		case '<':
			return true // both < and <<
		case '$':
			if i+1 < n && runes[i+1] == '(' {
				return true
			}
		}
	}

	return false
}

// higherRisk returns the higher of two risk level values.
func higherRisk(left, right string) string {
	order := map[string]int{riskSafe: 0, riskMedium: 1, riskHigh: 2}

	if _, ok := order[left]; !ok {
		left = riskMedium
	}
	if _, ok := order[right]; !ok {
		right = riskMedium
	}

	if order[right] > order[left] {
		return right
	}
	return left
}

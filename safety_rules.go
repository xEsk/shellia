package main

// shellOperatorRule treats any shell composition as requiring confirmation.
func shellOperatorRule(command string, tokens []string) (commandSafety, bool) {
	if hasShellOperators(command) {
		return riskySafety(), true
	}
	return commandSafety{}, false
}

// dangerousRootRule catches commands that are directly dangerous regardless of arguments.
func dangerousRootRule(command string, tokens []string) (commandSafety, bool) {
	dangerousRoots := map[string]bool{
		"sudo": true, "su": true, "rm": true, "dd": true, "mkfs": true, "shutdown": true,
		"reboot": true, "halt": true, "poweroff": true, "useradd": true, "adduser": true,
		"usermod": true, "userdel": true, "groupadd": true, "groupdel": true, "passwd": true,
		"chmod": true, "chown": true, "chgrp": true,
	}
	if dangerousRoots[tokens[0]] {
		return dangerousSafety(), true
	}
	return commandSafety{}, false
}

// gitRule allows only known read-only git subcommands to skip confirmation.
func gitRule(command string, tokens []string) (commandSafety, bool) {
	if len(tokens) < 2 || tokens[0] != "git" {
		return commandSafety{}, false
	}

	// "branch" excluded: `git branch -D` is destructive and subflags can't be checked here.
	safeGit := map[string]bool{
		"status": true, "log": true, "show": true, "diff": true, "rev-parse": true, "remote": true,
	}
	if safeGit[tokens[1]] {
		return safeSafety(), true
	}
	return riskySafety(), true
}

// dockerRule allows read-only Docker inspection commands to skip confirmation.
func dockerRule(command string, tokens []string) (commandSafety, bool) {
	if len(tokens) < 2 || tokens[0] != "docker" {
		return commandSafety{}, false
	}
	if isSafeDockerRead(tokens) {
		return safeSafety(), true
	}
	return commandSafety{}, false
}

// filesystemModificationRule catches commands that usually modify local files.
func filesystemModificationRule(command string, tokens []string) (commandSafety, bool) {
	if isFilesystemModification(tokens) {
		return riskySafety(), true
	}
	return commandSafety{}, false
}

// systemModificationRule catches commands that usually modify system, package, or service state.
func systemModificationRule(command string, tokens []string) (commandSafety, bool) {
	if isUserOrSystemModification(tokens) {
		return riskySafety(), true
	}
	return commandSafety{}, false
}

// findRule allows read-only find usage while requiring confirmation for mutating actions.
func findRule(command string, tokens []string) (commandSafety, bool) {
	if len(tokens) == 0 || tokens[0] != "find" {
		return commandSafety{}, false
	}
	if isMutatingFind(tokens) {
		return riskySafety(), true
	}
	return safeSafety(), true
}

// safeRootRule allows simple read-only commands to skip confirmation.
func safeRootRule(command string, tokens []string) (commandSafety, bool) {
	safeRoots := map[string]bool{
		"ls": true, "pwd": true, "cat": true, "echo": true, "whoami": true, "id": true,
		"uname": true, "date": true, "grep": true, "rg": true, "which": true,
		"whereis": true, "head": true, "tail": true, "wc": true, "stat": true,
		"du": true, "df": true, "ps": true, "man": true,
	}
	if safeRoots[tokens[0]] {
		return safeSafety(), true
	}
	return commandSafety{}, false
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

// isMutatingFind reports whether a find command contains actions that can modify files or ask for input.
func isMutatingFind(tokens []string) bool {
	if len(tokens) == 0 || tokens[0] != "find" {
		return false
	}

	mutatingActions := map[string]bool{
		"-delete":  true,
		"-exec":    true,
		"-execdir": true,
		"-ok":      true,
		"-okdir":   true,
	}
	for _, token := range tokens[1:] {
		if mutatingActions[token] {
			return true
		}
	}
	return false
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

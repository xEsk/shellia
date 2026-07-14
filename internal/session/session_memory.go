package session

import (
	"regexp"
	"strings"
)

var backtickCommandPattern = regexp.MustCompile("`([^`]+)`")

// updateSessionState stores durable session memory after a successful or actionable partial turn.
func updateSessionState(state *sessionState, instruction string, turn turnResult, cfg config) {
	if state == nil {
		return
	}

	rememberInstructionContext(state, instruction)

	if shouldPromotePendingIntent(instruction, turn) {
		state.PendingIntent = strings.TrimSpace(instruction)
	}

	if suggested := detectSuggestedCommand(turn); suggested != "" {
		state.LastSuggestedCommand = suggested
	} else if turn.Actionable {
		state.LastSuggestedCommand = ""
	}

	if hint := detectRuntimeHint(instruction, turn); hint != "" {
		state.LastRuntimeHint = hint
	}

	created := collectCreatedFiles(turn.Executions)
	if len(created) > 0 {
		state.LastCreatedFiles = mergeRecentUnique(state.LastCreatedFiles, created, 6)
		state.LastReferencedFile = created[len(created)-1]
	}

	if referenced := extractReferencedFiles(instruction); len(referenced) > 0 {
		state.LastReferencedFile = referenced[len(referenced)-1]
	}

	state.LastObservations = collectObservationMemory(turn.Executions, cfg.MemoryObservationChars, cfg.MaxObservationEntries, cfg.TruncationStrategy)
}

// updateSessionStateFromExecution updates reusable session memory after a manual shell command.
func updateSessionStateFromExecution(state *sessionState, command string, execution commandExecution, cfg config) {
	if state == nil {
		return
	}

	rememberInstructionContext(state, command)

	created := collectCreatedFiles([]commandExecution{execution})
	if len(created) > 0 {
		state.LastCreatedFiles = mergeRecentUnique(state.LastCreatedFiles, created, 6)
		state.LastReferencedFile = created[len(created)-1]
	}

	if referenced := extractReferencedFiles(command); len(referenced) > 0 {
		state.LastReferencedFile = referenced[len(referenced)-1]
	}

	if observations := collectObservationMemory([]commandExecution{execution}, cfg.MemoryObservationChars, cfg.MaxObservationEntries, cfg.TruncationStrategy); len(observations) > 0 {
		state.LastObservations = observations
	}

	state.LastSuggestedCommand = ""
}

// rememberUnfinishedInstruction keeps enough memory when a turn fails or is cancelled.
func rememberUnfinishedInstruction(state *sessionState, instruction string) {
	if state == nil {
		return
	}
	rememberInstructionContext(state, instruction)
	if shouldPromotePendingIntent(instruction, turnResult{}) {
		state.PendingIntent = strings.TrimSpace(instruction)
	}
}

// rememberInstructionContext updates lightweight memory derived from the raw instruction.
func rememberInstructionContext(state *sessionState, instruction string) {
	if state == nil {
		return
	}
	if hint := detectRuntimeHint(instruction, turnResult{}); hint != "" {
		state.LastRuntimeHint = hint
	}
	if referenced := extractReferencedFiles(instruction); len(referenced) > 0 {
		state.LastReferencedFile = referenced[len(referenced)-1]
	}
}

// resolveInstructionForPlanning preserves the user's text; the planning model
// resolves intent and follow-up references from the session memory in the prompt.
func resolveInstructionForPlanning(instruction string, _ sessionState) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return instruction
	}
	return instruction
}

// shouldPromotePendingIntent keeps the latest meaningful instruction available
// for the planning model, which decides later whether it is still relevant.
func shouldPromotePendingIntent(instruction string, _ turnResult) bool {
	return strings.TrimSpace(instruction) != ""
}

// detectRuntimeHint stores the latest runtime/container context when present.
func detectRuntimeHint(instruction string, turn turnResult) string {
	normalized := normalizeForMemory(instruction)
	if strings.Contains(normalized, "docker") || strings.Contains(normalized, "container") {
		return strings.TrimSpace(instruction)
	}
	if strings.Contains(normalized, "php8") || strings.Contains(normalized, "php 8") {
		return strings.TrimSpace(instruction)
	}
	for _, plan := range turn.Plans {
		command := normalizeForMemory(plan.Command)
		if strings.Contains(command, "docker") || strings.Contains(command, "php") {
			return strings.TrimSpace(instruction)
		}
	}
	for _, execution := range turn.Executions {
		if execution.ExitCode != 0 {
			continue
		}
		command := normalizeForMemory(execution.Command)
		if strings.Contains(command, "docker") || strings.Contains(command, "php") {
			return strings.TrimSpace(instruction)
		}
	}
	return ""
}

// collectCreatedFiles extracts simple created files from executed commands.
func collectCreatedFiles(executions []commandExecution) []string {
	created := make([]string, 0, len(executions))
	for _, execution := range executions {
		if execution.ExitCode != 0 {
			continue
		}
		tokens := strings.Fields(strings.TrimSpace(execution.Command))
		if len(tokens) < 2 {
			continue
		}
		switch tokens[0] {
		case "touch":
			created = append(created, sanitizePathToken(tokens[1]))
		case "mkdir":
			created = append(created, sanitizePathToken(tokens[1]))
		}
	}
	return created
}

// extractReferencedFiles finds simple path-like tokens in user instructions.
func extractReferencedFiles(text string) []string {
	tokens := strings.Fields(text)
	files := make([]string, 0, len(tokens))
	for _, token := range tokens {
		clean := sanitizePathToken(token)
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "/") || strings.Contains(clean, ".") {
			files = append(files, clean)
		}
	}
	return files
}

// sanitizePathToken strips surrounding punctuation from a likely path token.
func sanitizePathToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'`()[]{}<>,;:")
	return token
}

// mergeRecentUnique appends items while keeping only the most recent unique values.
func mergeRecentUnique(existing []string, incoming []string, limit int) []string {
	merged := append([]string{}, existing...)
	for _, item := range incoming {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		filtered := make([]string, 0, len(merged))
		for _, current := range merged {
			if current != item {
				filtered = append(filtered, current)
			}
		}
		merged = append(filtered, item)
		if len(merged) > limit {
			merged = merged[len(merged)-limit:]
		}
	}
	return merged
}

// normalizeForMemory simplifies text for broad intent heuristics.
func normalizeForMemory(text string) string {
	replacer := strings.NewReplacer(
		"'", "", "\"", "", "(", " ", ")", " ", ",", " ", ".", " ",
		":", " ", ";", " ", "?", " ", "!", " ", "-", " ",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(text))), " ")
}

// detectSuggestedCommand extracts the last shell command Shellia suggested in a
// non-actionable answer so short follow-ups can accept it explicitly.
func detectSuggestedCommand(turn turnResult) string {
	for _, source := range []string{turn.Result, turn.Summary} {
		matches := backtickCommandPattern.FindAllStringSubmatch(source, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			command := strings.TrimSpace(match[1])
			if looksLikeShellCommand(command) {
				return command
			}
		}
	}
	return ""
}

// looksLikeShellCommand applies a lightweight heuristic to backticked text to
// avoid storing arbitrary prose as a suggested command.
func looksLikeShellCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "\n") {
		return false
	}

	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return false
	}

	commandRoots := map[string]bool{
		"brew": true, "git": true, "docker": true, "ls": true, "pwd": true, "cat": true,
		"find": true, "rg": true, "grep": true, "touch": true, "mkdir": true, "rm": true,
		"mv": true, "cp": true, "python": true, "python3": true, "php": true, "node": true,
		"npm": true, "pnpm": true, "yarn": true, "go": true,
	}

	return commandRoots[tokens[0]]
}

// collectObservationMemory stores a compact reusable digest of the last observed outputs.
func collectObservationMemory(executions []commandExecution, chars int, maxEntries int, strategy truncationStrategy) []observationMemory {
	observations := make([]observationMemory, 0, len(executions))
	for _, execution := range executions {
		if execution.ExitCode == 0 && !execution.Stdout.HasOutput() && !execution.Stderr.HasOutput() {
			continue
		}
		transcript := strings.TrimSpace(execution.ObservationTranscript(chars, strategy))
		observations = append(observations, observationMemory{
			Command:    execution.Command,
			Purpose:    execution.Purpose,
			Transcript: transcript,
		})
	}

	if maxEntries > 0 && len(observations) > maxEntries {
		observations = observations[len(observations)-maxEntries:]
	}

	return observations
}

package main

import (
	"regexp"
	"strings"
)

var backtickCommandPattern = regexp.MustCompile("`([^`]+)`")

// updateSessionState stores durable session memory after a successful turn.
func updateSessionState(state *sessionState, instruction string, turn turnResult) {
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

	state.LastObservations = collectObservationMemory(turn.Executions)
}

// updateSessionStateFromExecution updates reusable session memory after a manual shell command.
func updateSessionStateFromExecution(state *sessionState, command string, execution commandExecution) {
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

	if observations := collectObservationMemory([]commandExecution{execution}); len(observations) > 0 {
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

// resolveInstructionForPlanning expands follow-up references using session memory.
func resolveInstructionForPlanning(instruction string, state sessionState) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return instruction
	}
	if looksLikeAffirmativeFollowUp(instruction) && strings.TrimSpace(state.LastSuggestedCommand) != "" {
		return "The user is accepting this previously suggested command: " + state.LastSuggestedCommand + ". Follow-up from user: " + instruction
	}
	if !looksLikeReferenceFollowUp(instruction) || strings.TrimSpace(state.PendingIntent) == "" {
		return instruction
	}
	return "Continue this pending task: " + state.PendingIntent + ". Follow-up from user: " + instruction
}

// shouldPromotePendingIntent decides whether the current instruction should become the active task.
func shouldPromotePendingIntent(instruction string, turn turnResult) bool {
	if strings.TrimSpace(instruction) == "" {
		return false
	}
	if len(turn.Plans) == 0 {
		return true
	}
	return !looksPreparatory(instruction)
}

// looksLikeReferenceFollowUp detects references such as "before", "do it", or "the docker thing".
func looksLikeReferenceFollowUp(input string) bool {
	normalized := normalizeForMemory(input)
	referenceSnippets := []string{
		"abans", "aixo", "això", "allo", "allò", "fesho", "fes ho", "fes ho", "fes ho ara",
		"do it", "do that", "that thing", "before", "earlier", "previous",
		"lo del", "el del", "the docker thing", "continue", "ara fes", "si", "yes",
		"ok", "okay", "vale", "d acord", "dacord", "llista", "lista",
	}
	for _, snippet := range referenceSnippets {
		if strings.Contains(normalized, snippet) {
			return true
		}
	}
	return false
}

// looksLikeAffirmativeFollowUp detects short confirmations that usually accept
// a previously suggested command.
func looksLikeAffirmativeFollowUp(input string) bool {
	normalized := normalizeForMemory(input)
	if normalized == "" {
		return false
	}

	affirmatives := []string{
		"ok", "okay", "vale", "si", "yes", "d acord", "dacord",
		"fesho", "fes ho", "fes ho ara", "do it", "go ahead",
		"llista", "lista", "endavant",
	}
	for _, snippet := range affirmatives {
		if normalized == snippet || strings.HasPrefix(normalized, snippet+" ") {
			return true
		}
	}
	return false
}

// looksPreparatory reports whether the instruction is likely a small supporting action.
func looksPreparatory(input string) bool {
	normalized := normalizeForMemory(input)
	prepPrefixes := []string{
		"crea ", "create ", "make ", "touch ", "mira ", "mostra ", "llista ",
		"show ", "list ", "cat ", "open ", "obre ", "inspect ", "check ",
		"create a test file", "crea un petit fitxer", "crea un fitxer",
	}
	for _, prefix := range prepPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
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
func collectObservationMemory(executions []commandExecution) []observationMemory {
	const (
		maxObservationEntries = 4
		observationChars      = 400
	)

	observations := make([]observationMemory, 0, len(executions))
	for _, execution := range executions {
		transcript := strings.TrimSpace(execution.PromptTranscript(observationChars))
		if transcript == "" || transcript == "Output: (empty)" {
			continue
		}
		observations = append(observations, observationMemory{
			Command:    execution.Command,
			Purpose:    execution.Purpose,
			Transcript: transcript,
		})
	}

	if len(observations) > maxObservationEntries {
		observations = observations[len(observations)-maxObservationEntries:]
	}

	return observations
}

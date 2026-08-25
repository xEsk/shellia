package session

import "strings"

// MemoryOptions contains the controls that shape retained observation memory.
type MemoryOptions struct {
	MaxObservationEntries  int
	MemoryObservationChars int
	TruncationStrategy     truncationStrategy
	PlanOnly               bool
}

// updateSessionState stores durable session memory after a successful or actionable partial turn.
func updateSessionState(state *sessionState, instruction string, turn turnResult, options MemoryOptions) {
	if state == nil {
		return
	}

	rememberInstructionContext(state, instruction)
	if (turn.Outcome == turnOutcomeCompleted || turn.Outcome == turnOutcomePlanned) && strings.TrimSpace(turn.Proposal.Objective) != "" {
		state.PendingProposal = pendingProposal{
			Mode:      turn.Proposal.Mode,
			Objective: strings.TrimSpace(turn.Proposal.Objective),
			Summary:   strings.TrimSpace(turn.Proposal.Summary),
		}
	} else if turn.Outcome != "" {
		state.PendingProposal = pendingProposal{}
	}

	switch turn.Outcome {
	case turnOutcomeCompleted, turnOutcomePlanned:
		state.PendingIntent = ""
		state.PendingIntentPlanOnly = false
		state.LastBlockerKind = ""
		state.LastBlockerReason = ""
	case turnOutcomeBlocked:
		state.PendingIntent = strings.TrimSpace(instruction)
		state.PendingIntentPlanOnly = options.PlanOnly
		state.LastBlockerKind = strings.TrimSpace(turn.BlockerKind)
		state.LastBlockerReason = strings.TrimSpace(turn.BlockerReason)
	default:
		if shouldPromotePendingIntent(instruction, turn) {
			state.PendingIntent = strings.TrimSpace(instruction)
			state.PendingIntentPlanOnly = options.PlanOnly
		}
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

	if len(turn.Executions) > 0 {
		state.LastObservationObjective = ""
	}
	if observations := collectObservationMemory(turn.Executions, options.MemoryObservationChars, options.MaxObservationEntries, options.TruncationStrategy); len(observations) > 0 {
		state.LastObservations = observations
		state.LastObservationObjective = strings.TrimSpace(instruction)
	} else if turn.Outcome == turnOutcomeCompleted {
		state.LastObservations = nil
		state.LastObservationObjective = ""
	}
}

// updateSessionStateFromExecution updates reusable session memory after a manual shell command.
func updateSessionStateFromExecution(state *sessionState, command string, execution commandExecution, options MemoryOptions) {
	if state == nil {
		return
	}
	state.LastObservationObjective = ""

	rememberInstructionContext(state, command)

	created := collectCreatedFiles([]commandExecution{execution})
	if len(created) > 0 {
		state.LastCreatedFiles = mergeRecentUnique(state.LastCreatedFiles, created, 6)
		state.LastReferencedFile = created[len(created)-1]
	}

	if referenced := extractReferencedFiles(command); len(referenced) > 0 {
		state.LastReferencedFile = referenced[len(referenced)-1]
	}

	if observations := collectObservationMemory([]commandExecution{execution}, options.MemoryObservationChars, options.MaxObservationEntries, options.TruncationStrategy); len(observations) > 0 {
		state.LastObservations = observations
		state.LastObservationObjective = ""
	}
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
func resolveInstructionForPlanning(instruction string, state sessionState) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return instruction
	}
	if strings.TrimSpace(state.PendingProposal.Objective) != "" && isProposalAcceptance(instruction) {
		return strings.TrimSpace(state.PendingProposal.Objective)
	}
	return instruction
}

// isProposalAcceptance recognizes only short, unequivocal acceptance phrases.
func isProposalAcceptance(instruction string) bool {
	switch normalizeForMemory(instruction) {
	case "si", "sí", "yes", "fes ho", "endavant", "executa ho", "executa l", "executal", "d acord", "ok", "okay":
		return true
	default:
		return false
	}
}

// isProposalDecline recognizes only short, unequivocal refusal phrases.
func isProposalDecline(instruction string) bool {
	switch normalizeForMemory(instruction) {
	case "no", "no gràcies", "ara no", "cancel·la", "cancella":
		return true
	default:
		return false
	}
}

// shouldPromotePendingIntent keeps the latest meaningful instruction available
// for the planning model, which decides later whether it is still relevant.
func shouldPromotePendingIntent(instruction string, turn turnResult) bool {
	return strings.TrimSpace(instruction) != "" && turn.Outcome != turnOutcomeCompleted && turn.Outcome != turnOutcomePlanned
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
		"'", "", "’", "", "\"", "", "(", " ", ")", " ", ",", " ", ".", " ",
		":", " ", ";", " ", "?", " ", "!", " ", "-", " ",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(text))), " ")
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

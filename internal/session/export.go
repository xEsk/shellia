package session

// UpdateState stores durable session memory after a successful turn.
func UpdateState(state *sessionState, instruction string, turn turnResult, cfg config) {
	updateSessionState(state, instruction, turn, cfg)
}

// UpdateStateFromExecution updates reusable session memory after a manual shell command.
func UpdateStateFromExecution(state *sessionState, command string, execution commandExecution, cfg config) {
	updateSessionStateFromExecution(state, command, execution, cfg)
}

// RememberUnfinishedInstruction keeps enough memory when a turn fails or is cancelled.
func RememberUnfinishedInstruction(state *sessionState, instruction string) {
	rememberUnfinishedInstruction(state, instruction)
}

// ResolveInstructionForPlanning preserves the user's text for planning.
func ResolveInstructionForPlanning(instruction string, state sessionState) string {
	return resolveInstructionForPlanning(instruction, state)
}

// IsProposalDecline reports whether the instruction clearly refuses a pending offer.
func IsProposalDecline(instruction string) bool {
	return isProposalDecline(instruction)
}

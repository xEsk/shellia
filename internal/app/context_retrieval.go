package app

import (
	"fmt"
	"strings"
)

const sessionContextBudgetChars = 16000

// retrieveContext resolves complete session results under the runtime-owned budget.
func (state *workflowState) retrieveContext(history []historyEntry, refs []string) (blockerKind, blockerReason string) {
	start := 0
	if len(history) > maxHistoryEntries {
		start = len(history) - maxHistoryEntries
	}
	entriesByID := make(map[string]historyEntry, len(history)-start)
	for _, entry := range history[start:] {
		entriesByID[entry.ID] = entry
	}

	selected := make([]historyEntry, 0, len(refs))
	required := 0
	for _, ref := range refs {
		entry, exists := entriesByID[ref]
		if !exists {
			return "missing_input", fmt.Sprintf("Session result %s is no longer available.", ref)
		}
		selected = append(selected, entry)
		required += len([]rune(entry.Result))
	}
	if required > sessionContextBudgetChars {
		return "unavailable", fmt.Sprintf("Session results %s require %d characters; the retrieval limit is %d.", strings.Join(refs, ", "), required, sessionContextBudgetChars)
	}

	state.contextRefs = append(state.contextRefs[:0], refs...)
	state.retrievedContext = append(state.retrievedContext[:0], selected...)
	state.contextRevision++
	return "", ""
}

// validateContextReferences requires the current revision and exact ordered unique references.
func (state *workflowState) validateContextReferences(revision int, refs []string) error {
	if state.contextRevision == 0 || revision != state.contextRevision {
		return fmt.Errorf("completion context revision %d does not match current context revision %d", revision, state.contextRevision)
	}
	if len(refs) != len(state.contextRefs) {
		return fmt.Errorf("completion context references do not match loaded context references")
	}
	seen := make(map[string]struct{}, len(refs))
	for index, ref := range refs {
		if _, duplicate := seen[ref]; duplicate {
			return fmt.Errorf("completion context references contain duplicate %q", ref)
		}
		seen[ref] = struct{}{}
		if ref != state.contextRefs[index] {
			return fmt.Errorf("completion context references do not match loaded context references")
		}
	}
	return nil
}

// retrievedContextCharacterCount returns the complete rune count of loaded results.
func retrievedContextCharacterCount(entries []historyEntry) int {
	total := 0
	for _, entry := range entries {
		total += len([]rune(entry.Result))
	}
	return total
}

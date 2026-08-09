package session

import "testing"

// TestMemoryOptionsRetainsObservationControls verifies session memory receives
// only the controls that shape stored observations.
func TestMemoryOptionsRetainsObservationControls(t *testing.T) {
	options := MemoryOptions{
		MaxObservationEntries:  2,
		MemoryObservationChars: 80,
		TruncationStrategy:     truncationMixed,
	}
	state := &sessionState{}

	UpdateState(state, "inspect", turnResult{}, options)
	UpdateStateFromExecution(state, "pwd", commandExecution{}, options)

	if options.MaxObservationEntries != 2 {
		t.Fatalf("MaxObservationEntries = %d, want 2", options.MaxObservationEntries)
	}
}

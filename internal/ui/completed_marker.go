package ui

// showCompletedMarker prints a compact success marker when the command produced no visible output.
func showCompletedMarker(box *stepBox, hadOutput bool) {
	if box == nil || box.closed || hadOutput {
		return
	}
	box.Section("completed", colorGreen)
}

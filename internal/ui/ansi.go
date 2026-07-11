package ui

import "strings"

// stripANSISequences removes CSI-style ANSI escape sequences from a string.
func stripANSISequences(text string) string {
	var b strings.Builder
	runes := []rune(text)

	for index := 0; index < len(runes); index++ {
		if runes[index] == '\033' && index+1 < len(runes) && runes[index+1] == '[' {
			index += 2
			for index < len(runes) && (runes[index] < '@' || runes[index] > '~') {
				index++
			}
			continue
		}
		if runes[index] == '\033' {
			continue
		}
		b.WriteRune(runes[index])
	}

	return b.String()
}

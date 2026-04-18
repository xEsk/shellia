package main

import (
	"strings"
	"unicode"
)

const (
	commandBoxPrompt            = "run ›"
	commandBoxHorizontalPadding = greyPanelHorizontalPadding
	commandBoxMinPanelWidth     = 24
	commandBoxBackground        = greyPanelBackground
	commandBoxForeground        = greyPanelForeground
	commandBoxPromptForeground  = "\033[38;5;75m"
)

// Command prints the command as a compact code panel inside the step box.
func (box *stepBox) Command(command string) {
	if box == nil {
		return
	}

	for _, line := range renderCommandBox(box.ui, command, box.innerWidth()) {
		box.writeRow(line)
	}
}

// renderCommandBox returns the rendered rows for a compact command panel.
func renderCommandBox(ui bool, command string, maxWidth int) []string {
	contentLines, panelWidth := commandBoxContent(command, maxWidth)
	if !ui {
		return renderPlainCommandBox(contentLines)
	}

	lines := make([]string, 0, len(contentLines)+2)
	blank := greyPanelBlankRow(panelWidth)
	lines = append(lines, blank)
	for index, line := range contentLines {
		lines = append(lines, commandBoxRenderedContentLine(line, panelWidth, index == 0))
	}
	lines = append(lines, blank)
	return lines
}

// commandBoxContent wraps the command and returns the panel width needed for it.
func commandBoxContent(command string, maxWidth int) ([]string, int) {
	panelWidth := commandBoxPanelWidth(command, maxWidth)
	wrapWidth := commandBoxWrapWidth(panelWidth)
	lines := wrapCommandText(command, wrapWidth)
	if len(lines) == 0 {
		lines = []string{""}
	}

	return lines, panelWidth
}

// commandBoxPanelWidth returns the smallest useful visible width for the command panel.
func commandBoxPanelWidth(command string, maxWidth int) int {
	minFeasibleWidth := commandBoxHorizontalPadding*2 + visibleWidth(commandBoxPrompt+" ") + 1
	if maxWidth < minFeasibleWidth {
		maxWidth = minFeasibleWidth
	}

	width := commandBoxHorizontalPadding*2 + visibleWidth(commandBoxPrompt+" "+command)
	if width < commandBoxMinPanelWidth && commandBoxMinPanelWidth <= maxWidth {
		width = commandBoxMinPanelWidth
	}
	if width > maxWidth {
		width = maxWidth
	}

	return width
}

// commandBoxWrapWidth returns how much command text fits after the prompt marker.
func commandBoxWrapWidth(panelWidth int) int {
	width := panelWidth - (commandBoxHorizontalPadding * 2) - visibleWidth(commandBoxPrompt+" ")
	if width < 1 {
		return 1
	}
	return width
}

// commandBoxRenderedContentLine returns one coloured row of command content.
func commandBoxRenderedContentLine(commandLine string, panelWidth int, first bool) string {
	if first {
		return greyPanelRow(panelWidth,
			greyPanelSegment{text: commandBoxPrompt, color: commandBoxPromptForeground},
			greyPanelSegment{text: " " + commandLine},
		)
	}

	return greyPanelRow(panelWidth,
		greyPanelSegment{text: strings.Repeat(" ", visibleWidth(commandBoxPrompt+" ")) + commandLine},
	)
}

// renderPlainCommandBox returns the no-colour fallback for the command panel.
func renderPlainCommandBox(contentLines []string) []string {
	lines := make([]string, 0, len(contentLines))
	for index, line := range contentLines {
		if index == 0 {
			lines = append(lines, commandBoxPrompt+" "+line)
			continue
		}
		lines = append(lines, strings.Repeat(" ", visibleWidth(commandBoxPrompt+" "))+line)
	}
	return lines
}

// wrapCommandText splits a command into visible lines while preserving shell spacing where possible.
func wrapCommandText(command string, width int) []string {
	if width <= 0 {
		return []string{command}
	}
	if command == "" {
		return []string{""}
	}

	runes := []rune(command)
	lines := make([]string, 0, 1)
	start := 0

	for start < len(runes) {
		remaining := len(runes) - start
		if remaining <= width {
			lines = append(lines, string(runes[start:]))
			break
		}

		end := start + width
		breakAt := -1
		for index := end - 1; index > start; index-- {
			if unicode.IsSpace(runes[index]) {
				breakAt = index + 1
				break
			}
		}
		if breakAt == -1 {
			breakAt = end
		}

		lines = append(lines, string(runes[start:breakAt]))
		start = breakAt
	}

	return lines
}

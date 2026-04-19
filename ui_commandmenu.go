package main

import "strings"

const (
	commandMenuBorderColor = "\033[38;5;242m"
	commandMenuPadding     = 2
)

// commandMenuLines renders the slash-command suggestions shown below the prompt.
func commandMenuLines(ui bool, input string) []string {
	suggestions := matchingInteractiveSlashCommands(input)
	if len(suggestions) == 0 {
		return nil
	}

	commandWidth := 0
	for _, suggestion := range suggestions {
		if width := visibleWidth(suggestion.Input); width > commandWidth {
			commandWidth = width
		}
	}

	descriptionWidth := 0
	for _, suggestion := range suggestions {
		if width := visibleWidth(suggestion.Description); width > descriptionWidth {
			descriptionWidth = width
		}
	}

	contentWidth := commandWidth + 2 + descriptionWidth
	boxWidth := contentWidth + commandMenuPadding*2 + 2
	lines := make([]string, 0, len(suggestions)+2)
	lines = append(lines, commandMenuBorderLine(ui, "╭", "╮", boxWidth))
	for _, suggestion := range suggestions {
		gap := strings.Repeat(" ", commandWidth-visibleWidth(suggestion.Input)+2)
		content := style(ui, colorWhite+colorBold, suggestion.Input) +
			gap +
			style(ui, colorDim, suggestion.Description)
		rightPadding := contentWidth - visibleWidth(suggestion.Input) - visibleWidth(gap) - visibleWidth(suggestion.Description)
		line := style(ui, commandMenuBorderColor, "│") +
			strings.Repeat(" ", commandMenuPadding) +
			content +
			strings.Repeat(" ", rightPadding+commandMenuPadding) +
			style(ui, commandMenuBorderColor, "│")
		lines = append(lines, line)
	}
	lines = append(lines, commandMenuBorderLine(ui, "╰", "╯", boxWidth))
	return lines
}

// commandMenuBorderLine renders one compact border row for the command menu.
func commandMenuBorderLine(ui bool, left string, right string, width int) string {
	innerWidth := width - 2
	if innerWidth < 0 {
		innerWidth = 0
	}
	return style(ui, commandMenuBorderColor, left+strings.Repeat("─", innerWidth)+right)
}

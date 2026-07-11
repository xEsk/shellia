package ui

import "strings"

const (
	commandMenuBorderColor = "\033[38;5;242m"
	commandMenuPadding     = 2
)

type commandMenuItem struct {
	Input       string
	Description string
}

// commandMenuLines renders the slash-command suggestions shown below the prompt.
func commandMenuLines(ui bool, input string, cfg config) []string {
	suggestions := commandMenuSuggestions(input, cfg)
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

// commandMenuSuggestions returns top-level slash commands or /model profile entries.
func commandMenuSuggestions(input string, cfg config) []commandMenuItem {
	if prefix, ok := modelMenuPrefix(input); ok {
		return modelMenuSuggestions(prefix, cfg)
	}

	matches := matchingInteractiveSlashCommands(input)
	if len(matches) == 0 {
		return nil
	}

	suggestions := make([]commandMenuItem, 0, len(matches))
	for _, match := range matches {
		suggestions = append(suggestions, commandMenuItem{
			Input:       match.Input,
			Description: match.Description,
		})
	}
	return suggestions
}

// modelMenuPrefix extracts the profile-name prefix when the prompt is editing /model.
func modelMenuPrefix(input string) (string, bool) {
	if input == "" || strings.TrimLeft(input, " \t") != input {
		return "", false
	}
	if strings.ContainsAny(input, "\r\n") {
		return "", false
	}

	lower := strings.ToLower(input)
	switch {
	case lower == "/model":
		return "", true
	case strings.HasPrefix(lower, "/model "):
		fields := strings.Fields(input)
		if len(fields) > 2 {
			return "", false
		}
		return strings.TrimSpace(input[len("/model"):]), true
	default:
		return "", false
	}
}

// modelMenuSuggestions renders configured model profiles for the /model submenu.
func modelMenuSuggestions(prefix string, cfg config) []commandMenuItem {
	if len(cfg.Models) == 0 {
		return []commandMenuItem{{
			Input:       "no models",
			Description: "configure [[models]] in config.toml",
		}}
	}

	prefix = strings.ToLower(strings.TrimSpace(prefix))
	suggestions := make([]commandMenuItem, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(model.Name), prefix) {
			continue
		}
		name := model.Name
		if model.Name == cfg.ModelName {
			name = "* " + name
		}
		suggestions = append(suggestions, commandMenuItem{
			Input:       name,
			Description: model.Model,
		})
	}
	return suggestions
}

// commandMenuBorderLine renders one compact border row for the command menu.
func commandMenuBorderLine(ui bool, left string, right string, width int) string {
	innerWidth := width - 2
	if innerWidth < 0 {
		innerWidth = 0
	}
	return style(ui, commandMenuBorderColor, left+strings.Repeat("─", innerWidth)+right)
}

// completeInteractiveCommand returns the first matching slash command or model profile.
func completeInteractiveCommand(input string, cfg config) (string, bool) {
	if prefix, ok := modelMenuPrefix(input); ok {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		for _, model := range cfg.Models {
			if prefix == "" || strings.HasPrefix(strings.ToLower(model.Name), prefix) {
				return "/model " + model.Name, true
			}
		}
		return "", false
	}
	return completeInteractiveSlashCommand(input)
}

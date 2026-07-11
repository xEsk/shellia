package interactive

import "strings"

type interactiveCommand string

const (
	interactiveCommandNone    interactiveCommand = ""
	interactiveCommandUnknown interactiveCommand = "unknown"
	interactiveCommandExit    interactiveCommand = "exit"
	interactiveCommandClear   interactiveCommand = "clear"
	interactiveCommandContext interactiveCommand = "context"
	interactiveCommandShell   interactiveCommand = "shell"
	interactiveCommandAI      interactiveCommand = "ai"
	interactiveCommandMode    interactiveCommand = "mode"
	interactiveCommandModel   interactiveCommand = "model"
	interactiveCommandPlan    interactiveCommand = "plan"
	interactiveCommandRetry   interactiveCommand = "retry"
	interactiveCommandNew     interactiveCommand = "new"
)

type interactiveCommandSpec struct {
	Input       string
	Command     interactiveCommand
	Description string
}

var interactiveSlashCommands = []interactiveCommandSpec{
	{Input: "/ai", Command: interactiveCommandAI, Description: "return to prompt mode"},
	{Input: "/shell", Command: interactiveCommandShell, Description: "enter direct shell mode"},
	{Input: "/plan", Command: interactiveCommandPlan, Description: "show a plan without running commands"},
	{Input: "/new", Command: interactiveCommandNew, Description: "start a fresh context"},
	{Input: "/retry", Command: interactiveCommandRetry, Description: "retry the last cancelled or failed request"},
	{Input: "/context", Command: interactiveCommandContext, Description: "show current local context"},
	{Input: "/mode", Command: interactiveCommandMode, Description: "show current mode"},
	{Input: "/model", Command: interactiveCommandModel, Description: "switch configured model profile"},
	{Input: "/clear", Command: interactiveCommandClear, Description: "clear the terminal"},
	{Input: "/exit", Command: interactiveCommandExit, Description: "close the session"},
}

// parsePlanInstruction extracts the instruction from a one-shot /plan prompt.
func parsePlanInstruction(input string) (bool, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false, ""
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "/plan" {
		return false, ""
	}

	return true, strings.TrimSpace(trimmed[len(fields[0]):])
}

// parseInteractiveCommand maps control commands to session actions.
func parseInteractiveCommand(input string) interactiveCommand {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return interactiveCommandNone
	}
	if normalized == "exit" {
		return interactiveCommandExit
	}

	for _, spec := range interactiveSlashCommands {
		if normalized == spec.Input || (spec.Command == interactiveCommandModel && strings.HasPrefix(normalized, spec.Input+" ")) {
			return spec.Command
		}
	}

	firstField := strings.Fields(normalized)
	if len(firstField) > 0 && strings.HasPrefix(firstField[0], "/") && !strings.Contains(firstField[0][1:], "/") {
		return interactiveCommandUnknown
	}
	return interactiveCommandNone
}

// matchingInteractiveSlashCommands returns commands matching the current slash prefix.
func matchingInteractiveSlashCommands(input string) []interactiveCommandSpec {
	if input == "" || strings.TrimLeft(input, " \t") != input {
		return nil
	}
	if strings.ContainsAny(input, " \t\r\n") {
		return nil
	}

	prefix := strings.ToLower(input)
	if !strings.HasPrefix(prefix, "/") || strings.Contains(prefix[1:], "/") {
		return nil
	}

	matches := make([]interactiveCommandSpec, 0, len(interactiveSlashCommands))
	for _, spec := range interactiveSlashCommands {
		if strings.HasPrefix(spec.Input, prefix) {
			matches = append(matches, spec)
		}
	}
	return matches
}

// completeInteractiveSlashCommand returns the first matching slash command.
func completeInteractiveSlashCommand(input string) (string, bool) {
	matches := matchingInteractiveSlashCommands(input)
	if len(matches) == 0 {
		return "", false
	}
	return matches[0].Input, true
}

// parseModelCommandName extracts the optional profile name from a /model command.
func parseModelCommandName(input string) string {
	trimmed := strings.TrimSpace(input)
	fields := strings.Fields(trimmed)
	if len(fields) < 2 || strings.ToLower(fields[0]) != "/model" {
		return ""
	}
	return fields[1]
}

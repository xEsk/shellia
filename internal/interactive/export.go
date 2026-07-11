package interactive

// Command is a parsed interactive slash command.
type Command = interactiveCommand

// CommandSpec describes one supported slash command.
type CommandSpec = interactiveCommandSpec

const (
	CommandNone    = interactiveCommandNone
	CommandUnknown = interactiveCommandUnknown
	CommandExit    = interactiveCommandExit
	CommandClear   = interactiveCommandClear
	CommandContext = interactiveCommandContext
	CommandRetry   = interactiveCommandRetry
	CommandNew     = interactiveCommandNew
	CommandShell   = interactiveCommandShell
	CommandAI      = interactiveCommandAI
	CommandMode    = interactiveCommandMode
	CommandModel   = interactiveCommandModel
	CommandPlan    = interactiveCommandPlan
)

// ParsePlanInstruction extracts `/plan <instruction>`.
func ParsePlanInstruction(input string) (bool, string) {
	return parsePlanInstruction(input)
}

// ParseCommand parses an interactive slash command.
func ParseCommand(input string) Command {
	return parseInteractiveCommand(input)
}

// MatchingSlashCommands returns slash commands matching the current prefix.
func MatchingSlashCommands(input string) []CommandSpec {
	return matchingInteractiveSlashCommands(input)
}

// CompleteSlashCommand completes a slash command prefix when possible.
func CompleteSlashCommand(input string) (string, bool) {
	return completeInteractiveSlashCommand(input)
}

// ParseModelCommandName extracts the profile name from `/model <name>`.
func ParseModelCommandName(input string) string {
	return parseModelCommandName(input)
}

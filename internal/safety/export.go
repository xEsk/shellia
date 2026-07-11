package safety

// CommandSafety is the local safety classification for one shell command.
type CommandSafety = commandSafety

// ClassifyCommand applies a conservative local policy to each command.
func ClassifyCommand(command string) CommandSafety {
	return classifyCommand(command)
}

// HigherRisk returns the stricter risk level.
func HigherRisk(left, right string) string {
	return higherRisk(left, right)
}

// HasShellOperators reports whether a command contains shell control operators.
func HasShellOperators(command string) bool {
	return hasShellOperators(command)
}

const (
	// ClassificationSafe is the local safe classification label.
	ClassificationSafe      = classificationSafe
	ClassificationDangerous = classificationDangerous
	RiskSafe                = riskSafe
	RiskHigh                = riskHigh
)

package main

import "strings"

// Risk level constants. Used across classifyCommand, higherRisk, riskBadge.
const (
	riskSafe   = "safe"
	riskMedium = "medium"
	riskHigh   = "high"
)

// Classification constants. Used across classifyCommand, classificationBadge, normalizePlan.
const (
	classificationSafe      = "safe"
	classificationRisky     = "risky"
	classificationDangerous = "dangerous"
)

type commandSafety struct {
	Classification       string
	Risk                 string
	RequiresConfirmation bool
}

type safetyRule func(command string, tokens []string) (commandSafety, bool)

var safetyRules = []safetyRule{
	shellOperatorRule,
	dangerousRootRule,
	gitRule,
	dockerRule,
	filesystemModificationRule,
	systemModificationRule,
	findRule,
	safeRootRule,
}

// classifyCommand applies a conservative local policy to each command.
func classifyCommand(command string) commandSafety {
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return dangerousSafety()
	}

	for _, rule := range safetyRules {
		if result, ok := rule(command, tokens); ok {
			return result
		}
	}

	return riskySafety()
}

// safeSafety returns the standard safe classification.
func safeSafety() commandSafety {
	return commandSafety{Classification: classificationSafe, Risk: riskSafe, RequiresConfirmation: false}
}

// riskySafety returns the standard confirmation-required classification.
func riskySafety() commandSafety {
	return commandSafety{Classification: classificationRisky, Risk: riskMedium, RequiresConfirmation: true}
}

// dangerousSafety returns the standard high-risk classification.
func dangerousSafety() commandSafety {
	return commandSafety{Classification: classificationDangerous, Risk: riskHigh, RequiresConfirmation: true}
}

// hasShellOperators detects shell operators and command separators outside of quoted strings.
// Prevents false positives like: echo "a > b" or grep 'a|b'.
func hasShellOperators(command string) bool {
	inSingle := false
	inDouble := false
	runes := []rune(command)
	n := len(runes)

	for i := 0; i < n; i++ {
		ch := runes[i]

		if inSingle {
			// Inside single quotes there are no escapes; only ' closes.
			if ch == '\'' {
				inSingle = false
			}
			continue
		}

		if inDouble {
			// Inside double quotes, \ escapes the next character.
			if ch == '\\' {
				i++
				continue
			}
			if ch == '"' {
				inDouble = false
			}
			continue
		}

		// Outside quotes.
		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '\\':
			i++ // skip escaped character
		case ';', '`', '\n', '\r':
			return true
		case '|':
			return true // both | and ||
		case '&':
			return true // background jobs and &&
		case '>':
			return true // both > and >>
		case '<':
			return true // both < and <<
		case '$':
			if i+1 < n && runes[i+1] == '(' {
				return true
			}
		}
	}

	return false
}

// higherRisk returns the higher of two risk level values.
func higherRisk(left, right string) string {
	order := map[string]int{riskSafe: 0, riskMedium: 1, riskHigh: 2}

	if _, ok := order[left]; !ok {
		left = riskMedium
	}
	if _, ok := order[right]; !ok {
		right = riskMedium
	}

	if order[right] > order[left] {
		return right
	}
	return left
}

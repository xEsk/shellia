package safety

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

type shellSyntax struct {
	roots       []string
	hasOperator bool
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
	ripgrepRule,
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

// scanShellSyntax finds shell operators and executable roots with shared quote and escape handling.
func scanShellSyntax(command string) shellSyntax {
	result := shellSyntax{roots: make([]string, 0, 2)}
	runes := []rune(command)

	var scanExecutable func(start int, terminator rune, escapedBacktick bool) int
	scanExecutable = func(start int, terminator rune, escapedBacktick bool) int {
		word := make([]rune, 0, 16)
		quote := rune(0)
		captureQuotedWord := false
		expectRoot := true
		parenthesisDepth := 0

		flushWord := func() {
			if len(word) == 0 {
				return
			}
			if expectRoot {
				if isShellAssignmentPrefix(word) {
					word = word[:0]
					return
				}
				result.roots = append(result.roots, string(word))
				expectRoot = false
			}
			word = word[:0]
		}

		for index := start; index < len(runes); index++ {
			ch := runes[index]

			if quote == '\'' {
				if ch == '\'' {
					quote = 0
					captureQuotedWord = false
				} else if captureQuotedWord {
					word = append(word, ch)
				}
				continue
			}

			if quote == '"' {
				switch ch {
				case '\\':
					if index+1 < len(runes) {
						index++
						if captureQuotedWord {
							word = append(word, runes[index])
						}
					}
				case '"':
					quote = 0
					captureQuotedWord = false
				case '`':
					result.hasOperator = true
					index = scanExecutable(index+1, '`', false)
				case '$':
					if index+1 < len(runes) && runes[index+1] == '(' {
						result.hasOperator = true
						index = scanExecutable(index+2, ')', false)
					} else if captureQuotedWord {
						word = append(word, ch)
					}
				default:
					if captureQuotedWord {
						word = append(word, ch)
					}
				}
				continue
			}

			if escapedBacktick && ch == '\\' && index+1 < len(runes) && runes[index+1] == '`' {
				flushWord()
				return index + 1
			}
			if ch == terminator && terminator == '`' {
				flushWord()
				return index
			}
			if ch == ')' && terminator == ')' {
				if parenthesisDepth == 0 {
					flushWord()
					return index
				}
				parenthesisDepth--
				result.hasOperator = true
				flushWord()
				expectRoot = true
				continue
			}

			switch ch {
			case '\'':
				quote = '\''
				captureQuotedWord = terminator != 0 && expectRoot
				if !captureQuotedWord {
					flushWord()
				}
			case '"':
				quote = '"'
				captureQuotedWord = terminator != 0 && expectRoot
				if !captureQuotedWord {
					flushWord()
				}
			case '\\':
				if terminator == '`' && index+1 < len(runes) && runes[index+1] == '`' {
					result.hasOperator = true
					flushWord()
					index = scanExecutable(index+2, '`', true)
					continue
				}
				if index+1 < len(runes) {
					index++
					word = append(word, runes[index])
				}
			case '$':
				if index+1 < len(runes) && runes[index+1] == '(' {
					result.hasOperator = true
					flushWord()
					index = scanExecutable(index+2, ')', false)
					continue
				}
				word = append(word, ch)
			case '`':
				result.hasOperator = true
				flushWord()
				index = scanExecutable(index+1, '`', false)
			case ';', '\n', '\r', '|', '&':
				result.hasOperator = true
				flushWord()
				expectRoot = true
				if index+1 < len(runes) && runes[index+1] == ch {
					index++
				}
			case '(', ')':
				result.hasOperator = true
				flushWord()
				expectRoot = true
				if ch == '(' && terminator == ')' {
					parenthesisDepth++
				}
			case '>', '<':
				result.hasOperator = true
			case ' ', '\t':
				flushWord()
			default:
				word = append(word, ch)
			}
		}

		flushWord()
		return len(runes)
	}

	scanExecutable(0, 0, false)
	return result
}

// isShellAssignmentPrefix reports whether a word can precede an executable command root.
func isShellAssignmentPrefix(word []rune) bool {
	for index, ch := range word {
		if ch == '=' {
			return index > 0
		}

		isLetter := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if ch != '_' && !isLetter && (index == 0 || !isDigit) {
			return false
		}
	}

	return false
}

// commandRoots returns executable-looking command roots, including roots inside substitutions.
func commandRoots(command string) []string {
	return scanShellSyntax(command).roots
}

// hasDangerousCommandRoot reports whether any command root is always high risk.
func hasDangerousCommandRoot(command string) bool {
	for _, root := range commandRoots(command) {
		if isDangerousRoot(root) {
			return true
		}
	}
	return false
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

// hasShellOperators detects shell operators, separators, and executable substitutions.
// Prevents false positives like: echo "a > b" or grep 'a|b'.
func hasShellOperators(command string) bool {
	return scanShellSyntax(command).hasOperator
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

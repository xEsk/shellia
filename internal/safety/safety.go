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
	primaryRoot string
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

	var scanExecutable func(start int, terminator rune, escapedBacktick bool, topLevel bool) int
	scanExecutable = func(start int, terminator rune, escapedBacktick bool, topLevel bool) int {
		word := make([]rune, 0, 16)
		quote := rune(0)
		captureQuotedWord := false
		expectRoot := true
		transparentWrapper := ""
		wrapperValueMode := ""
		primaryCandidate := ""
		firstStage := topLevel
		parenthesisDepth := 0

		flushWord := func() {
			if len(word) == 0 {
				return
			}
			if expectRoot {
				token := string(word)
				if wrapperValueMode != "" {
					valueMode := wrapperValueMode
					wrapperValueMode = ""
					if valueMode == "split" {
						if root := envSplitCommandRoot(token); root != "" {
							token = root
							result.roots = append(result.roots, token)
							if firstStage {
								primaryCandidate = token
							}
							expectRoot = false
							transparentWrapper = ""
						}
					}
					word = word[:0]
					return
				}
				if isShellAssignmentPrefix(word) {
					word = word[:0]
					return
				}
				if transparentWrapper != "" {
					if skip, valueMode := transparentWrapperOption(transparentWrapper, token); skip {
						wrapperValueMode = valueMode
						word = word[:0]
						return
					}
				}
				result.roots = append(result.roots, token)
				if firstStage {
					primaryCandidate = token
				}
				transparentWrapper = transparentCommandWrapper(token)
				expectRoot = transparentWrapper != ""
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
					index = scanExecutable(index+1, '`', false, false)
				case '$':
					if index+1 < len(runes) && runes[index+1] == '(' {
						result.hasOperator = true
						index = scanExecutable(index+2, ')', false, false)
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
				captureQuotedWord = expectRoot
				if !captureQuotedWord {
					flushWord()
				}
			case '"':
				quote = '"'
				captureQuotedWord = expectRoot
				if !captureQuotedWord {
					flushWord()
				}
			case '\\':
				if terminator == '`' && index+1 < len(runes) && runes[index+1] == '`' {
					result.hasOperator = true
					flushWord()
					index = scanExecutable(index+2, '`', true, false)
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
					index = scanExecutable(index+2, ')', false, false)
					continue
				}
				word = append(word, ch)
			case '`':
				result.hasOperator = true
				flushWord()
				index = scanExecutable(index+1, '`', false, false)
			case ';', '\n', '\r', '|', '&':
				result.hasOperator = true
				flushWord()
				if firstStage {
					result.primaryRoot = primaryCandidate
					firstStage = false
				}
				expectRoot = true
				transparentWrapper = ""
				wrapperValueMode = ""
				if index+1 < len(runes) && runes[index+1] == ch {
					index++
				}
			case '(', ')':
				result.hasOperator = true
				flushWord()
				expectRoot = true
				transparentWrapper = ""
				wrapperValueMode = ""
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
		if firstStage {
			result.primaryRoot = primaryCandidate
		}
		return len(runes)
	}

	scanExecutable(0, 0, false, true)
	return result
}

// transparentCommandWrapper identifies prefixes that execute another command.
func transparentCommandWrapper(root string) string {
	if separator := strings.LastIndexByte(root, '/'); separator >= 0 {
		root = root[separator+1:]
	}
	if root == "env" {
		return root
	}
	return ""
}

// transparentWrapperOption skips env configuration before its executable root.
func transparentWrapperOption(wrapper string, token string) (bool, string) {
	if wrapper != "env" || !strings.HasPrefix(token, "-") {
		return false, ""
	}
	if token == "-S" || token == "--split-string" {
		return true, "split"
	}
	needsValue := token == "-u" || token == "--unset" || token == "-C" || token == "--chdir" || token == "-P"
	if needsValue {
		return true, "discard"
	}
	return true, ""
}

// envSplitCommandRoot resolves the executable after env reprocesses a -S value.
func envSplitCommandRoot(value string) string {
	fields := splitEnvString(value)
	for index := 0; index < len(fields); index++ {
		token := fields[index]
		if isShellAssignmentPrefix([]rune(token)) {
			continue
		}
		if skip, valueMode := transparentWrapperOption("env", token); skip {
			if valueMode == "discard" && index+1 < len(fields) {
				index++
			}
			continue
		}
		return token
	}
	return ""
}

// splitEnvString tokenizes the quotes and escapes accepted by env -S values.
func splitEnvString(value string) []string {
	fields := make([]string, 0, 4)
	word := make([]rune, 0, len(value))
	quote := rune(0)
	escaped := false
	flush := func() {
		if len(word) == 0 {
			return
		}
		fields = append(fields, string(word))
		word = word[:0]
	}

	for _, ch := range []rune(value) {
		if escaped {
			word = append(word, ch)
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word = append(word, ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			word = append(word, ch)
		}
	}
	if escaped {
		word = append(word, '\\')
	}
	flush()
	return fields
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

// primaryCommandRoot returns the effective source command in the first shell stage.
func primaryCommandRoot(command string) string {
	return scanShellSyntax(command).primaryRoot
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

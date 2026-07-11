package ui

import (
	"strings"

	glamour "charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

var (
	answerMarkdownTrue  = true
	answerMarkdownFalse = false
	answerMarkdownWhite = "15"
	answerMarkdownDim   = "8"
	answerMarkdownCyan  = "14"
)

// renderAnswerMarkdown renders simple Markdown into terminal-ready answer lines.
func renderAnswerMarkdown(message string, width int, ui bool) []string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}

	rendered, ok := renderAnswerMarkdownWithGlamour(trimmed, width, ui)
	if !ok {
		return fallbackAnswerLines(message, width, ui)
	}

	lines := trimTrailingBlankLines(splitRenderedAnswerLines(rendered))
	if len(lines) == 0 || !answerLinesFit(lines, width) {
		return fallbackAnswerLines(message, width, ui)
	}
	return lines
}

func renderAnswerMarkdownWithGlamour(message string, width int, ui bool) (string, bool) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(answerMarkdownStyle(ui)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", false
	}

	rendered, err := renderer.Render(message)
	if err != nil {
		return "", false
	}
	return rendered, true
}

func answerMarkdownStyle(ui bool) ansi.StyleConfig {
	if !ui {
		return ansi.StyleConfig{
			List: ansi.StyleList{
				LevelIndent: 2,
			},
			Item: ansi.StylePrimitive{
				BlockPrefix: "- ",
			},
			Enumeration: ansi.StylePrimitive{
				BlockPrefix: ". ",
			},
			Task: ansi.StyleTask{
				Ticked:   "[x] ",
				Unticked: "[ ] ",
			},
		}
	}

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &answerMarkdownWhite,
				Bold:  &answerMarkdownTrue,
			},
		},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &answerMarkdownCyan,
				Bold:  &answerMarkdownTrue,
			},
		},
		Emph: ansi.StylePrimitive{
			Italic: &answerMarkdownTrue,
		},
		Strong: ansi.StylePrimitive{
			Bold: &answerMarkdownTrue,
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: &answerMarkdownTrue,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "- ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[x] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     &answerMarkdownCyan,
			Underline: &answerMarkdownTrue,
		},
		LinkText: ansi.StylePrimitive{
			Color: &answerMarkdownCyan,
			Bold:  &answerMarkdownTrue,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           &answerMarkdownWhite,
				BackgroundColor: &answerMarkdownDim,
				Bold:            &answerMarkdownFalse,
			},
		},
	}
}

func splitRenderedAnswerLines(rendered string) []string {
	rendered = strings.Trim(rendered, "\r\n")
	if rendered == "" {
		return nil
	}

	rawLines := strings.Split(rendered, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, trimRenderedAnswerLineRight(strings.TrimSuffix(line, "\r")))
	}
	return lines
}

func trimRenderedAnswerLineRight(line string) string {
	runes := []rune(line)
	i := len(runes)
	suffix := ""

	for i > 0 {
		if start, ok := trailingANSISequenceStart(runes, i); ok {
			suffix = string(runes[start:i]) + suffix
			i = start
			continue
		}
		if runes[i-1] == ' ' || runes[i-1] == '\t' {
			i--
			continue
		}
		break
	}

	return string(runes[:i]) + suffix
}

func trailingANSISequenceStart(runes []rune, end int) (int, bool) {
	if end < 3 {
		return 0, false
	}
	final := runes[end-1]
	if final < '@' || final > '~' {
		return 0, false
	}
	for i := end - 2; i >= 0; i-- {
		if runes[i] == '[' && i > 0 && runes[i-1] == '\033' {
			return i - 1, true
		}
		if runes[i] >= '@' && runes[i] <= '~' {
			return 0, false
		}
	}
	return 0, false
}

func answerLinesFit(lines []string, width int) bool {
	for _, line := range lines {
		if visibleWidth(line) > width {
			return false
		}
	}
	return true
}

func fallbackAnswerLines(message string, width int, ui bool) []string {
	lines := layoutAnswerLines(message, width)
	if !ui {
		return lines
	}

	for index, line := range lines {
		lines[index] = style(ui, colorWhite+colorBold, line)
	}
	return lines
}

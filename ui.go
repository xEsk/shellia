package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[97m"
)

type prefixedWriter struct {
	box     *stepBox
	hidden  bool
	started bool
	buffer  string
}

type directShellWriter struct {
	ui        bool
	target    io.Writer
	lineStart bool
	started   bool
}

// Write applies a visual prefix to each line of shell output.
// It returns len(data) on success, or 0 + error if writing to the target fails.
func (writer *prefixedWriter) Write(data []byte) (int, error) {
	if writer.hidden {
		return len(data), nil
	}

	writer.buffer += string(data)
	for len(writer.buffer) > 0 {
		if !writer.started {
			if writer.box != nil {
				writer.box.OutputLabel()
			}
			writer.started = true
		}

		newlineIndex := strings.IndexByte(writer.buffer, '\n')
		if newlineIndex == -1 {
			return len(data), nil
		}

		chunk := strings.TrimSuffix(writer.buffer[:newlineIndex+1], "\n")
		if writer.box != nil {
			writer.box.OutputLine(chunk)
		}
		writer.buffer = writer.buffer[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush forces printing of the last partial line in the buffer when present.
func (writer *prefixedWriter) Flush() error {
	if writer.hidden {
		return nil
	}

	if !writer.started || writer.buffer == "" {
		return nil
	}
	if writer.box != nil {
		writer.box.OutputLine(writer.buffer)
	}
	writer.buffer = ""
	return nil
}

// Write prints /shell mode output directly with a more subdued tone.
// It keeps lines aligned at column zero and paints each line in gray.
func (writer *directShellWriter) Write(data []byte) (int, error) {
	text := string(data)
	for len(text) > 0 {
		if writer.lineStart {
			if !writer.started {
				if _, err := fmt.Fprint(writer.target, "\n"); err != nil {
					return 0, err
				}
				writer.started = true
			}
			if _, err := fmt.Fprint(writer.target, styleStart(writer.ui, colorDim)); err != nil {
				return 0, err
			}
			writer.lineStart = false
		}

		newlineIndex := strings.IndexByte(text, '\n')
		if newlineIndex == -1 {
			if _, err := fmt.Fprint(writer.target, text); err != nil {
				return 0, err
			}
			return len(data), nil
		}

		chunk := text[:newlineIndex]
		if _, err := fmt.Fprint(writer.target, chunk, styleEnd(writer.ui), "\n"); err != nil {
			return 0, err
		}
		writer.lineStart = true
		text = text[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush closes the last partial line of /shell mode when present.
func (writer *directShellWriter) Flush() error {
	if writer.lineStart {
		return nil
	}
	_, err := fmt.Fprint(writer.target, styleEnd(writer.ui))
	return err
}

// uiEnabled reports whether enriched output can use ANSI colours.
func uiEnabled(cfg config) bool {
	if cfg.NoColor {
		return false
	}

	termName := strings.TrimSpace(os.Getenv("TERM"))
	if termName == "" || termName == "dumb" {
		return false
	}

	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}

// exitWithError prints an error and terminates the process with the given code.
func exitWithError(ui bool, message string, code int) {
	renderPanel(os.Stderr, ui, "error", colorRed, []string{
		style(ui, colorWhite+colorBold, message),
	})
	os.Exit(code)
}

// printContext shows the detected context when debug mode is enabled.
func printContext(ui bool, ctxInfo contextInfo) {
	lines := []string{
		metaLine(ui, "cwd", ctxInfo.CWD),
		metaLine(ui, "user", ctxInfo.User),
		metaLine(ui, "os", ctxInfo.OS),
		metaLine(ui, "shell", ctxInfo.Shell),
		metaLine(ui, "git", strconv.FormatBool(ctxInfo.Git.IsRepo)),
		metaLine(ui, "branch", fallbackValue(ctxInfo.Git.Branch, "-")),
	}
	if ctxInfo.Git.StatusShort == "" {
		lines = append(lines, metaLine(ui, "status", style(ui, colorDim, "(clean or empty)")))
	} else {
		lines = append(lines, fmt.Sprintf("%s\n%s", metaLabel(ui, "status"), indentLines(ctxInfo.Git.StatusShort, shellStreamPrefix(ui))))
	}
	renderPanel(os.Stdout, ui, "context", colorBlue, lines)
}

// printPlan presents the summary and the commands proposed by the model.
func printPlan(ui bool, cfg config, summary string, plans []commandPlan, discovery bool) {
	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	renderPanel(os.Stdout, ui, title, titleColor, []string{style(ui, colorWhite+colorBold, summary)})

	if len(plans) == 0 || !cfg.Verbose {
		return
	}

	lines := make([]string, 0, len(plans)+1)
	for index, plan := range plans {
		lines = append(lines,
			fmt.Sprintf("%s %s", stepBadge(ui, index+1), plan.Purpose),
			fmt.Sprintf("%s %s", metaLabel(ui, "command"), style(ui, colorWhite, plan.Command)),
			fmt.Sprintf("%s %s", metaLabel(ui, "risk"), riskBadge(ui, plan.Risk)),
			fmt.Sprintf("%s %s", metaLabel(ui, "safety"), classificationBadge(ui, plan.Classification)),
			fmt.Sprintf("%s %s", metaLabel(ui, "confirm"), confirmBadge(ui, plan.RequiresConfirmation)),
			"",
		)
	}
	renderPanel(os.Stdout, ui, "steps", colorDim, trimTrailingBlankLines(lines))
}

// printHeader shows a compact header with the global session state.
func printHeader(ui bool, ctxInfo contextInfo) {
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false) + style(ui, colorDim, " · ") + shelliaVersionBadge(ui))
	fmt.Println(style(ui, colorDim, fmt.Sprintf("%s · %s", ctxInfo.CWD, plainHeaderGitValue(ctxInfo))))
}

// printSection draws a section header with stronger visual hierarchy.
func printSection(ui bool, title string, color string) {
	fmt.Println()
	fmt.Println(style(ui, color+colorBold, title))
}

// printCommandExecution presents the active command inside a step box before running it.
func printCommandExecution(ui bool, cfg config, index int, total int, plan commandPlan) *stepBox {
	box := newStepBox(os.Stdout, ui, fmt.Sprintf("step %d/%d", index, total))
	box.Spacer()
	box.Command(plan.Command)
	box.Spacer()
	box.Bullet(plan.Purpose)
	if plan.Interactive {
		box.KeyValue("interactive", fallbackValue(plan.InteractiveReason, "yes"), colorYellow, colorWhite)
	}
	if cfg.Verbose {
		box.KeyValue("risk", plainRiskLabel(plan.Risk), colorYellow, colorWhite)
	}
	return box
}

// printInfo shows a short informational message.
func printInfo(ui bool, message string) {
	fmt.Printf("%s %s\n", shelliaBrand(ui, false), style(ui, colorWhite+colorBold, message))
}

// printModeStatus shows an interactive mode state change with cleaner output.
func printModeStatus(ui bool, message string) {
	renderPanel(os.Stdout, ui, "mode", colorCyan, []string{
		style(ui, colorWhite+colorBold, message),
	})
}

// printWarning shows a non-fatal warning.
func printWarning(ui bool, message string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", style(ui, colorYellow+colorBold, "warning"), style(ui, colorWhite+colorBold, message))
}

// printSeparator shows the standard horizontal separator used between sections.
func printSeparator(target io.Writer, ui bool) {
	fmt.Fprintln(target, style(ui, colorDim, strings.Repeat("─", boxWidth())))
}

// printFinalResult shows the final useful answer for the user (non-streaming fallback).
func printFinalResult(ui bool, message string) {
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false))
	renderAnswerBlock(os.Stdout, ui, message, nil)
	fmt.Println()
	printSeparator(os.Stdout, ui)
}

// openResultPanel opens the streaming result block.
func openResultPanel(ui bool) {
	fmt.Println()
	printSeparator(os.Stdout, ui)
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false))
}

// closeResultPanel visually closes the streaming result.
func closeResultPanel(ui bool) {
	fmt.Print(styleEnd(ui))
	fmt.Println()
	fmt.Println()
	printSeparator(os.Stdout, ui)
}

// resultWriter wraps os.Stdout to prefix each line with a subtle result indent.
// wroteAnything tracks whether any content has been sent to the terminal,
// regardless of whether lineStart is currently true or false.
type resultWriter struct {
	ui            bool
	wroteAnything bool
	buffer        strings.Builder
	state         answerRenderState
	thinking      *thinkingIndicator
}

func (writer *resultWriter) Write(data []byte) (int, error) {
	if writer.thinking != nil {
		writer.thinking.stop()
		writer.thinking = nil
	}

	if _, err := writer.buffer.Write(data); err != nil {
		return 0, err
	}

	if len(layoutAnswerLines(writer.buffer.String(), answerContentWidth(writer.ui))) > 0 {
		writer.wroteAnything = true
		if err := renderAnswerBlock(os.Stdout, writer.ui, writer.buffer.String(), &writer.state); err != nil {
			return 0, err
		}
	}

	return len(data), nil
}

func (writer *resultWriter) stopThinking() {
	if writer == nil || writer.thinking == nil {
		return
	}
	writer.thinking.stop()
	writer.thinking = nil
}

type answerRenderState struct {
	rows int
}

// clearRenderedAnswer clears the previously rendered Shellia answer block.
func clearRenderedAnswer(target io.Writer, state *answerRenderState) {
	if state == nil || state.rows == 0 {
		return
	}

	fmt.Fprint(target, "\r")
	if state.rows > 1 {
		fmt.Fprintf(target, "\033[%dA", state.rows-1)
	}
	for index := 0; index < state.rows; index++ {
		fmt.Fprint(target, "\033[2K")
		if index < state.rows-1 {
			fmt.Fprint(target, "\033[1B\r")
		}
	}
	if state.rows > 1 {
		fmt.Fprintf(target, "\033[%dA", state.rows-1)
	}
	fmt.Fprint(target, "\r")
	state.rows = 0
}

// answerContentWidth returns the available content width for Shellia answers.
func answerContentWidth(ui bool) int {
	width := boxWidth() - visibleWidth(answerPrefix(ui))
	if width < 1 {
		return 1
	}
	return width
}

// layoutAnswerLines wraps the Shellia answer by words while preserving explicit blank lines.
func layoutAnswerLines(message string, width int) []string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}

	paragraphs := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		content := strings.TrimSpace(paragraph)
		if content == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapPlainText(content, width)...)
	}

	return trimTrailingBlankLines(lines)
}

// renderAnswerBlock renders the Shellia answer with consistent left padding and word wrapping.
func renderAnswerBlock(target io.Writer, ui bool, message string, state *answerRenderState) error {
	lines := layoutAnswerLines(message, answerContentWidth(ui))
	clearRenderedAnswer(target, state)
	if len(lines) == 0 {
		return nil
	}

	prefix := answerPrefix(ui)
	for index, line := range lines {
		if index > 0 {
			if _, err := fmt.Fprint(target, "\r\n"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(target, prefix, style(ui, colorWhite+colorBold, line)); err != nil {
			return err
		}
	}

	if state != nil {
		state.rows = len(lines)
	}
	return nil
}

type editableRenderState struct {
	rows      int
	cursorRow int
}

type cursorAffinity int

const (
	cursorAffinityForward cursorAffinity = iota
	cursorAffinityBackward
)

// readInteractivePrompt shows a clear prompt and returns the entered text.
func readInteractivePrompt(ui bool, reader *bufio.Reader, mode interactiveMode, showCommandPopup bool) (string, error) {
	fmt.Println()
	fmt.Println(promptQuestionLine(ui, mode))
	prompt := promptPrefix(ui, mode)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Print(prompt)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		return strings.TrimSpace(line), nil
	}
	defer term.Restore(fd, state) //nolint:errcheck

	buffer := make([]rune, 0, 128)
	cursor := 0
	affinity := cursorAffinityForward
	single := []byte{0}
	renderState := &editableRenderState{}

	promptWidth := visibleWidth(prompt)
	contentWidth := promptRenderWidth() - promptWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	renderEditablePrompt(ui, prompt, buffer, cursor, affinity, renderState, showCommandPopup)

	for {
		_, err := os.Stdin.Read(single)
		if err != nil {
			return "", err
		}

		switch single[0] {
		case '\r', '\n':
			if !promptHasText(buffer) {
				if len(buffer) > 0 {
					buffer = buffer[:0]
					cursor = 0
					affinity = cursorAffinityForward
					renderEditablePrompt(ui, prompt, buffer, cursor, affinity, renderState, showCommandPopup)
				}
				continue
			}
			submitted := strings.TrimSpace(string(buffer))
			clearEditablePrompt(renderState)
			printSubmittedPrompt(ui, prompt, buffer)
			fmt.Print("\r\n")
			return submitted, nil
		case 3:
			clearEditablePrompt(renderState)
			printSubmittedPrompt(ui, prompt, buffer)
			fmt.Print("\r\n")
			return "exit", nil
		case 27:
			exitPrompt, err := applyEscapeSequenceOrExit(fd, &buffer, &cursor, contentWidth, &affinity)
			if err != nil {
				return "", err
			}
			if exitPrompt {
				clearEditablePrompt(renderState)
				printSubmittedPrompt(ui, prompt, buffer)
				fmt.Print("\r\n")
				return "exit", nil
			}
		case 127, 8:
			if cursor == 0 || len(buffer) == 0 {
				continue
			}
			buffer = append(buffer[:cursor-1], buffer[cursor:]...)
			cursor--
			affinity = cursorAffinityForward
		case '\t':
			if completed, ok := completeInteractiveSlashCommand(string(buffer)); ok {
				buffer = []rune(completed + " ")
				cursor = len(buffer)
				affinity = cursorAffinityForward
			}
		default:
			r, err := readInputRune(single[0])
			if err != nil {
				if errors.Is(err, errDiscardRune) {
					continue
				}
				return "", err
			}
			buffer = append(buffer[:cursor], append([]rune{r}, buffer[cursor:]...)...)
			cursor++
			affinity = cursorAffinityForward
		}

		renderEditablePrompt(ui, prompt, buffer, cursor, affinity, renderState, showCommandPopup)
	}
}

// promptHasText reports whether the editable prompt contains a real submission.
func promptHasText(buffer []rune) bool {
	return strings.TrimSpace(string(buffer)) != ""
}

// printSubmittedPrompt redraws only the submitted prompt and removes transient UI.
func printSubmittedPrompt(ui bool, prompt string, buffer []rune) {
	lines, _, _ := editablePromptLayout(prompt, buffer, len(buffer), cursorAffinityForward, promptRenderWidth())
	promptWidth := visibleWidth(prompt)

	for index, line := range lines {
		if index > 0 {
			fmt.Print("\r\n")
			fmt.Print(strings.Repeat(" ", promptWidth))
		} else {
			fmt.Print(prompt)
		}
		fmt.Print(style(ui, colorWhite, line))
	}
}

// promptQuestion returns the guiding text for the current interactive mode.
func promptQuestion(mode interactiveMode) string {
	if mode == interactiveModeShell {
		return "Enter a shell command to run."
	}
	return "What do you want Shellia to do?"
}

// promptQuestionLine renders the prompt question with the Shellia brand integrated.
func promptQuestionLine(ui bool, mode interactiveMode) string {
	if mode == interactiveModeShell {
		return style(ui, colorDim, promptQuestion(mode))
	}

	if !ui {
		return promptQuestion(mode)
	}

	return style(ui, colorDim, "What do you want ") +
		shelliaBrand(ui, false) +
		style(ui, colorDim, " to do?")
}

// promptPrefix renders the visual prompt prefix for the current mode.
func promptPrefix(ui bool, mode interactiveMode) string {
	if mode == interactiveModeShell {
		return style(ui, colorWhite+colorBold, "shell") + style(ui, colorWhite, " › ")
	}
	return style(ui, colorCyan+colorBold, "you") + style(ui, colorWhite, " › ")
}

type confirmDecision int

const (
	confirmDecisionCancel confirmDecision = iota
	confirmDecisionRun
	confirmDecisionEdit
	confirmDecisionInteractive
)

// renderConfirmationPrompt prints the confirmation prompt and highlights the configured default.
func renderConfirmationPrompt(box *stepBox, prompt string, defaultChoice confirmationDefault) {
	if box == nil {
		return
	}
	rendered := style(box.ui, colorYellow+colorBold, "• confirm ") +
		style(box.ui, colorWhite, prompt) +
		style(box.ui, colorWhite, " ") +
		renderConfirmationOptions(box.ui, defaultChoice) +
		style(box.ui, colorWhite, ": ")
	box.writeRow(rendered)
}

// logConfirmationChoice records the confirmation decision on the same line as the original prompt.
func logConfirmationChoice(box *stepBox, prompt string, defaultChoice confirmationDefault, choice string) {
	if box == nil {
		return
	}
	rendered := style(box.ui, colorYellow+colorBold, "• confirm ") +
		style(box.ui, colorWhite, prompt) +
		style(box.ui, colorWhite, " ") +
		renderConfirmationOptions(box.ui, defaultChoice) +
		style(box.ui, colorWhite, ": ") +
		style(box.ui, colorYellow+colorBold, choice)
	box.ReplaceLastRenderedRow(rendered)
}

// promptConfirmation asks for explicit confirmation inside the step box.
func promptConfirmation(box *stepBox, reader *bufio.Reader, prompt string, initialCommand string, defaultChoice confirmationDefault) (confirmDecision, string, error) {
	renderConfirmationPrompt(box, prompt, defaultChoice)

	key, ok, err := readSingleConfirmationKey()
	if err == nil && ok {
		for {
			if isConfirmationEnterKey(key) && defaultChoice != confirmationDefaultNone {
				return applyConfirmationChoice(box, reader, prompt, initialCommand, defaultChoice, defaultChoice)
			}

			lower := strings.ToLower(string(key))
			if choice, found := parseConfirmationChoice(lower); found {
				return applyConfirmationChoice(box, reader, prompt, initialCommand, defaultChoice, choice)
			}

			key, ok, err = readSingleConfirmationKey()
			if err != nil || !ok {
				break
			}
		}
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return confirmDecisionCancel, "", err
		}

		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			if defaultChoice != confirmationDefaultNone {
				return applyConfirmationChoice(box, reader, prompt, initialCommand, defaultChoice, defaultChoice)
			}
			if errors.Is(err, io.EOF) {
				logConfirmationChoice(box, prompt, defaultChoice, "no")
				return confirmDecisionCancel, "", nil
			}
			continue
		}

		if choice, found := parseConfirmationChoice(answer); found {
			return applyConfirmationChoice(box, reader, prompt, initialCommand, defaultChoice, choice)
		}

		logConfirmationChoice(box, prompt, defaultChoice, "no")
		return confirmDecisionCancel, "", nil
	}
}

// renderConfirmationOptions renders the accepted confirmation keys and highlights the default key.
func renderConfirmationOptions(ui bool, defaultChoice confirmationDefault) string {
	options := []struct {
		key    string
		choice confirmationDefault
	}{
		{key: "y", choice: confirmationDefaultYes},
		{key: "e", choice: confirmationDefaultEdit},
		{key: "i", choice: confirmationDefaultInteractive},
		{key: "n", choice: confirmationDefaultNo},
	}

	var builder strings.Builder
	builder.WriteString("[")
	for index, option := range options {
		if index > 0 {
			builder.WriteString("/")
		}
		color := colorWhite
		if option.choice == defaultChoice {
			color = colorYellow + colorBold
		}
		builder.WriteString(style(ui, color, option.key))
	}
	builder.WriteString("]")
	return builder.String()
}

// parseConfirmationChoice maps user input to a supported confirmation action.
func parseConfirmationChoice(value string) (confirmationDefault, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return confirmationDefaultYes, true
	case "e", "edit":
		return confirmationDefaultEdit, true
	case "i", "interactive":
		return confirmationDefaultInteractive, true
	case "n", "no":
		return confirmationDefaultNo, true
	default:
		return confirmationDefaultNone, false
	}
}

// applyConfirmationChoice applies a parsed confirmation action and logs it in the prompt row.
func applyConfirmationChoice(box *stepBox, reader *bufio.Reader, prompt string, initialCommand string, defaultChoice confirmationDefault, choice confirmationDefault) (confirmDecision, string, error) {
	switch choice {
	case confirmationDefaultYes:
		logConfirmationChoice(box, prompt, defaultChoice, "yes")
		return confirmDecisionRun, "", nil
	case confirmationDefaultEdit:
		logConfirmationChoice(box, prompt, defaultChoice, "edit")
		edited, editErr := box.EditCommand(reader, initialCommand)
		if editErr != nil {
			return confirmDecisionCancel, "", editErr
		}
		if strings.TrimSpace(edited) == "" {
			return confirmDecisionCancel, "", nil
		}
		return confirmDecisionEdit, edited, nil
	case confirmationDefaultInteractive:
		logConfirmationChoice(box, prompt, defaultChoice, "interactive")
		return confirmDecisionInteractive, "", nil
	default:
		logConfirmationChoice(box, prompt, defaultChoice, "no")
		return confirmDecisionCancel, "", nil
	}
}

// isConfirmationEnterKey reports whether a raw terminal key is Enter.
func isConfirmationEnterKey(key byte) bool {
	return key == '\r' || key == '\n'
}

// readSingleConfirmationKey tries to read a single key without waiting for Enter.
func readSingleConfirmationKey() (byte, bool, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return 0, false, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false, nil
	}
	defer term.Restore(fd, state) //nolint:errcheck

	buffer := []byte{0}
	_, err = os.Stdin.Read(buffer)
	if err != nil {
		return 0, false, err
	}

	return buffer[0], true, nil
}

var errDiscardRune = errors.New("discard rune")

// readInputRune decodes a complete UTF-8 rune from the first byte already read.
func readInputRune(first byte) (rune, error) {
	if first < utf8.RuneSelf {
		if first < 32 && first != '\t' {
			return 0, errDiscardRune
		}
		return rune(first), nil
	}

	size := utf8SequenceLength(first)
	if size == 0 {
		return 0, errDiscardRune
	}

	buf := make([]byte, size)
	buf[0] = first
	for index := 1; index < size; index++ {
		if _, err := os.Stdin.Read(buf[index : index+1]); err != nil {
			return 0, err
		}
	}

	r, decoded := utf8.DecodeRune(buf)
	if r == utf8.RuneError && decoded == 1 {
		return 0, errDiscardRune
	}
	return r, nil
}

// utf8SequenceLength returns the expected length of a UTF-8 sequence from its first byte.
func utf8SequenceLength(first byte) int {
	switch {
	case first&0b1110_0000 == 0b1100_0000:
		return 2
	case first&0b1111_0000 == 0b1110_0000:
		return 3
	case first&0b1111_1000 == 0b1111_0000:
		return 4
	default:
		return 0
	}
}

// wrapPromptRunesWithOffsets splits the buffer using the available width and
// also returns the starting offset of each line within the original buffer.
// It keeps trailing spaces so rendering and the caret share the exact same visual model.
func wrapPromptRunesWithOffsets(buffer []rune, width int) ([]string, []int) {
	if width < 1 {
		width = 1
	}
	if len(buffer) == 0 {
		return []string{""}, []int{0}
	}

	lines := make([]string, 0, len(buffer)/width+1)
	offsets := make([]int, 0, len(buffer)/width+1)
	start := 0
	for start < len(buffer) {
		offsets = append(offsets, start)
		remaining := len(buffer) - start
		if remaining <= width {
			lines = append(lines, string(buffer[start:]))
			break
		}

		lastSpace := -1
		for index := 0; index < width; index++ {
			if unicode.IsSpace(buffer[start+index]) {
				lastSpace = index
			}
		}

		chunkWidth := width
		if lastSpace > 0 {
			chunkWidth = lastSpace + 1
		}

		lines = append(lines, string(buffer[start:start+chunkWidth]))
		start += chunkWidth
	}

	if len(lines) == 0 {
		return []string{""}, []int{0}
	}
	return lines, offsets
}

// promptCursorPosition computes the caret row and column within the wrapped
// prompt layout from the offsets of each line.
// Affinity allows a line boundary to be represented as the end of the previous
// row or as the start of the next one, depending on where the movement came from.
func promptCursorPosition(lines []string, offsets []int, cursor int, affinity cursorAffinity) (int, int) {
	if len(lines) == 0 || len(offsets) == 0 {
		return 0, 0
	}

	if cursor < 0 {
		cursor = 0
	}
	lastRow := len(lines) - 1
	maxCursor := offsets[lastRow] + len([]rune(lines[lastRow]))
	if cursor > maxCursor {
		cursor = maxCursor
	}

	if affinity == cursorAffinityBackward {
		for row := 1; row < len(offsets); row++ {
			if cursor == offsets[row] {
				return row - 1, len([]rune(lines[row-1]))
			}
		}
	}

	row := 0
	for index := len(offsets) - 1; index >= 0; index-- {
		if cursor >= offsets[index] {
			row = index
			break
		}
	}

	col := cursor - offsets[row]
	lineLen := len([]rune(lines[row]))
	if col > lineLen {
		col = lineLen
	}
	return row, col
}

// moveCursorVertical moves the cursor one row up (delta=-1) or down (delta=+1)
// while trying to preserve the same visual column.
func moveCursorVertical(buffer []rune, cursor int, contentWidth int, delta int, affinity cursorAffinity) (int, cursorAffinity) {
	lines, offsets := wrapPromptRunesWithOffsets(buffer, contentWidth)
	if len(lines) <= 1 {
		if delta < 0 {
			return 0, cursorAffinityForward
		}
		if delta > 0 {
			return len(buffer), cursorAffinityForward
		}
		return cursor, affinity
	}

	cursorRow, cursorCol := promptCursorPosition(lines, offsets, cursor, affinity)

	targetRow := cursorRow + delta
	if targetRow < 0 {
		return 0, cursorAffinityForward
	}
	if targetRow >= len(lines) {
		return len(buffer), cursorAffinityForward
	}

	col := cursorCol
	if targetLineLen := len([]rune(lines[targetRow])); col > targetLineLen {
		col = targetLineLen
	}

	targetCursor := offsets[targetRow] + col
	targetAffinity := cursorAffinityForward
	if targetRow < len(lines)-1 && col == len([]rune(lines[targetRow])) {
		targetAffinity = cursorAffinityBackward
	}

	return targetCursor, targetAffinity
}

// applyEscapeSequence handles special cursor and editing keys.
// contentWidth is the width of the editable content (without the prefix); if it is 0
// vertical movement is disabled (for example in the single-line editor).
func applyEscapeSequence(buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) error {
	sequence := []byte{0, 0}
	if _, err := os.Stdin.Read(sequence[:1]); err != nil {
		return err
	}
	if sequence[0] != '[' {
		return nil
	}
	if _, err := os.Stdin.Read(sequence[1:2]); err != nil {
		return err
	}

	switch sequence[1] {
	case 'A':
		if contentWidth > 0 {
			currentAffinity := cursorAffinityForward
			if affinity != nil {
				currentAffinity = *affinity
			}
			nextCursor, nextAffinity := moveCursorVertical(*buffer, *cursor, contentWidth, -1, currentAffinity)
			*cursor = nextCursor
			if affinity != nil {
				*affinity = nextAffinity
			}
		}
	case 'B':
		if contentWidth > 0 {
			currentAffinity := cursorAffinityForward
			if affinity != nil {
				currentAffinity = *affinity
			}
			nextCursor, nextAffinity := moveCursorVertical(*buffer, *cursor, contentWidth, +1, currentAffinity)
			*cursor = nextCursor
			if affinity != nil {
				*affinity = nextAffinity
			}
		}
	case 'C':
		if *cursor < len(*buffer) {
			*cursor = *cursor + 1
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'D':
		if *cursor > 0 {
			*cursor = *cursor - 1
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'H':
		*cursor = 0
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'F':
		*cursor = len(*buffer)
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case '3':
		tilde := []byte{0}
		if _, err := os.Stdin.Read(tilde); err != nil {
			return err
		}
		if tilde[0] == '~' && *cursor < len(*buffer) {
			*buffer = append((*buffer)[:*cursor], (*buffer)[*cursor+1:]...)
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	}

	return nil
}

// applyEscapeSequenceOrExit interprets an escape sequence or closes the prompt if Esc is pressed alone.
func applyEscapeSequenceOrExit(fd int, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (bool, error) {
	ready, err := isInputReady(fd)
	if err != nil {
		return false, err
	}
	if !ready {
		return true, nil
	}
	return false, applyEscapeSequence(buffer, cursor, contentWidth, affinity)
}

// renderEditablePrompt repaints the full editable prompt block while handling wrapping correctly.
func renderEditablePrompt(ui bool, prompt string, buffer []rune, cursor int, affinity cursorAffinity, state *editableRenderState, showCommandPopup bool) {
	lines, cursorRow, cursorCol := editablePromptLayout(prompt, buffer, cursor, affinity, promptRenderWidth())
	var menuLines []string
	if showCommandPopup {
		menuLines = commandMenuLines(ui, string(buffer))
	}
	promptWidth := visibleWidth(prompt)

	clearEditablePrompt(state)

	for index, line := range lines {
		if index > 0 {
			fmt.Print("\r\n")
		}
		if index == 0 {
			fmt.Print(prompt)
		} else {
			fmt.Print(strings.Repeat(" ", promptWidth))
		}
		fmt.Print(style(ui, colorWhite, line))
	}

	for _, line := range menuLines {
		fmt.Print("\r\n")
		fmt.Print(line)
	}

	rows := len(lines) + len(menuLines)
	rowsBelow := rows - 1 - cursorRow
	fmt.Print("\r")
	if rowsBelow > 0 {
		fmt.Printf("\033[%dA", rowsBelow)
	}
	if cursorCol > 0 {
		fmt.Printf("\033[%dC", cursorCol)
	}

	if state != nil {
		state.rows = rows
		state.cursorRow = cursorRow
	}
}

// clearEditablePrompt clears all rows from the previously rendered prompt.
func clearEditablePrompt(state *editableRenderState) {
	if state == nil || state.rows == 0 {
		return
	}

	fmt.Print("\r")
	if state.cursorRow > 0 {
		fmt.Printf("\033[%dA", state.cursorRow)
	}
	for index := 0; index < state.rows; index++ {
		fmt.Print("\033[2K")
		if index < state.rows-1 {
			fmt.Print("\033[1B\r")
		}
	}
	if state.rows > 1 {
		fmt.Printf("\033[%dA", state.rows-1)
	}
	fmt.Print("\r")
}

// editablePromptLayout computes the visible lines and cursor position for the editable prompt.
func editablePromptLayout(prompt string, buffer []rune, cursor int, affinity cursorAffinity, width int) ([]string, int, int) {
	if width < 2 {
		width = 2
	}

	promptWidth := visibleWidth(prompt)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buffer) {
		cursor = len(buffer)
	}

	contentWidth := width - promptWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	lines, offsets := wrapPromptRunesWithOffsets(buffer, contentWidth)
	cursorRow, contentCol := promptCursorPosition(lines, offsets, cursor, affinity)
	cursorCol := promptWidth + contentCol
	return lines, cursorRow, cursorCol
}

// promptRenderWidth returns the usable width for the editable prompt while avoiding the last column.
func promptRenderWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		if width, _, err := term.GetSize(fd); err == nil && width > 1 {
			return width - 1
		}
	}
	return 79
}

// wrapPromptRunes splits prompt text into lines, prioritising word boundaries
// while keeping all spaces from the buffer.
func wrapPromptRunes(buffer []rune, width int) []string {
	lines, _ := wrapPromptRunesWithOffsets(buffer, width)
	return lines
}

// clearScreen clears the current terminal.
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// renderPanel draws a light visual block to better distinguish the Shellia UI.
func renderPanel(target io.Writer, ui bool, title string, color string, lines []string) {
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ui, color+colorBold, title))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(target)
			continue
		}
		for _, part := range strings.Split(line, "\n") {
			fmt.Fprintf(target, "%s%s\n", style(ui, colorDim, "  "), part)
		}
	}
}

// plainHeaderGitValue generates a short plain Git summary without styles.
func plainHeaderGitValue(ctxInfo contextInfo) string {
	if !ctxInfo.Git.IsRepo {
		return "not a repository"
	}

	branch := fallbackValue(ctxInfo.Git.Branch, "DETACHED")
	if strings.TrimSpace(ctxInfo.Git.StatusShort) == "" {
		return branch + " (clean)"
	}
	return branch + " (dirty)"
}

// shellStreamPrefix returns the prefix used for each line of real shell output.
func shellStreamPrefix(ui bool) string {
	return style(ui, colorDim, "│   ")
}

// answerPrefix returns the visual margin used for Shellia's main answer.
func answerPrefix(ui bool) string {
	return style(ui, colorGreen, "  ")
}

// shelliaBrand renders the Shellia brand with an accented "ia".
func shelliaBrand(ui bool, lower bool) string {
	left := "Shell"
	if lower {
		left = "shell"
	}
	return style(ui, colorWhite+colorBold, left) + style(ui, colorCyan+colorBold, "ia")
}

// shelliaVersionBadge renders the current version with compact visual treatment.
func shelliaVersionBadge(ui bool) string {
	return badge(ui, colorCyan, fallbackValue(strings.TrimSpace(version), "dev"))
}

// plainRiskLabel returns the risk level without ANSI styling.
func plainRiskLabel(risk string) string {
	switch risk {
	case riskSafe:
		return "safe"
	case riskHigh:
		return "high"
	default:
		return "medium"
	}
}

// style applies ANSI styling when the output supports it.
func style(ui bool, color string, text string) string {
	if !ui {
		return text
	}
	return color + text + colorReset
}

// styleStart returns only the ANSI prefix for a style.
func styleStart(ui bool, color string) string {
	if !ui {
		return ""
	}
	return color
}

// styleEnd returns the ANSI reset sequence when UI styling is enabled.
func styleEnd(ui bool) string {
	if !ui {
		return ""
	}
	return colorReset
}

// badge generates a compact visual label for the interface.
func badge(ui bool, color string, text string) string {
	return style(ui, color+colorBold, text)
}

// metaLabel renders a lightweight metadata label.
func metaLabel(ui bool, text string) string {
	return style(ui, colorDim, strings.ToLower(text))
}

// metaLine renders compact key/value metadata.
func metaLine(ui bool, key string, value string) string {
	return fmt.Sprintf("%s %s", metaLabel(ui, key), value)
}

// stepBadge generates a compact label for each plan step.
func stepBadge(ui bool, step int) string {
	return style(ui, colorBlue+colorBold, fmt.Sprintf("%d.", step))
}

// riskBadge renders the risk level with a colour-coded label.
func riskBadge(ui bool, risk string) string {
	switch risk {
	case riskSafe:
		return badge(ui, colorGreen, "safe")
	case riskHigh:
		return badge(ui, colorRed, "high")
	default:
		return badge(ui, colorYellow, "medium")
	}
}

// classificationBadge renders the local command classification.
func classificationBadge(ui bool, classification string) string {
	switch classification {
	case classificationSafe:
		return badge(ui, colorGreen, "safe")
	case classificationDangerous:
		return badge(ui, colorRed, "dangerous")
	default:
		return badge(ui, colorYellow, "risky")
	}
}

// confirmBadge clearly indicates whether the command requires confirmation.
func confirmBadge(ui bool, required bool) string {
	if required {
		return badge(ui, colorYellow, "required")
	}
	return badge(ui, colorGreen, "auto")
}

// fallbackValue returns a fallback text when the value is empty.
func fallbackValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// indentLines indents a multiline block to present it better in the terminal.
func indentLines(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// isRetryInstruction detects when the user wants to repeat the last unfinished attempt.
func isRetryInstruction(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	switch normalized {
	case "again", "retry", "try again", "do it again", "torna-ho a provar", "torna ho a provar", "fes-ho de nou", "fes ho de nou":
		return true
	default:
		return false
	}
}

// trimTrailingBlankLines removes extra trailing blank lines from a block.
func trimTrailingBlankLines(lines []string) []string {
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 {
		return nil
	}
	return lines[:last+1]
}

// boxMinWidth guarantees a readable minimum width even on narrow terminals.
const boxMinWidth = 40

// boxHorizontalMargin leaves a bit of space to the right of the box.
const boxHorizontalMargin = 4

// boxWidth computes the total width of the step box using the current terminal size.
func boxWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		width, _, err := term.GetSize(fd)
		if err == nil && width > 0 {
			width -= boxHorizontalMargin
			if width < boxMinWidth {
				return boxMinWidth
			}
			return width
		}
	}
	return 80
}

// visibleWidth computes the visible width of a string while ignoring ANSI sequences.
// CSI sequences have the form \033[ <params> <final>, where the final byte is 0x40–0x7E.
// The '[' character (0x5B) is the CSI introducer and must NOT be treated as a final byte.
func visibleWidth(text string) int {
	width := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == '\033' && i+1 < len(runes) && runes[i+1] == '[' {
			// CSI sequence: \033[ ... final_byte (0x40–0x7E)
			i += 2 // skip \033 and [
			for i < len(runes) && !(runes[i] >= '@' && runes[i] <= '~') {
				i++
			}
			i++ // skip the final byte
			continue
		}
		if r == '\033' {
			i++ // skip a lone ESC
			continue
		}
		width++
		i++
	}
	return width
}

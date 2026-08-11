package ui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
func uiEnabled(options ViewOptions) bool {
	if options.NoColor {
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

// printErrorTo renders a process-level error on the provided stream.
func printErrorTo(target io.Writer, ui bool, message string) {
	renderPanel(target, ui, "error", colorRed, []string{
		style(ui, colorWhite+colorBold, message),
	})
}

// printContext shows the detected context when debug mode is enabled.
func printContext(ui bool, options ViewOptions, ctxInfo contextInfo) {
	printContextTo(os.Stdout, ui, options, ctxInfo)
}

// printContextTo shows the detected context on the provided target.
func printContextTo(target io.Writer, ui bool, options ViewOptions, ctxInfo contextInfo) {
	lines := make([]string, 0, 4)
	if options.IncludeCWD {
		lines = append(lines, metaLine(ui, "cwd", ctxInfo.CWD))
	}
	if options.IncludeUser {
		lines = append(lines, metaLine(ui, "user", ctxInfo.User))
	}
	if options.IncludeOS {
		lines = append(lines, metaLine(ui, "os", ctxInfo.OS))
	}
	if options.IncludeShell {
		lines = append(lines, metaLine(ui, "shell", ctxInfo.Shell))
	}
	if len(lines) == 0 {
		lines = append(lines, style(ui, colorDim, "No local context shared by configuration."))
	}
	renderPanel(target, ui, "context", colorBlue, lines)
}

// printPlan presents the summary and the commands proposed by the model.
func printPlan(ui bool, options ViewOptions, summary string, plans []commandPlan, discovery bool) {
	printPlanTo(os.Stdout, ui, options, summary, plans, discovery)
}

// printPlanTo presents the summary and the commands proposed by the model on the provided target.
func printPlanTo(target io.Writer, ui bool, options ViewOptions, summary string, plans []commandPlan, discovery bool) {
	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	renderPanel(target, ui, title, titleColor, []string{style(ui, colorWhite+colorBold, summary)})

	if len(plans) == 0 || (!options.Verbose && !options.PlanOnly && !options.AskConfirmPlan) {
		return
	}

	lines := planStepLines(target, ui, options, plans)
	renderPanel(target, ui, "steps", colorDim, trimTrailingBlankLines(lines))
}

// planStepLines renders planned steps with compact command boxes in plan-only mode.
func planStepLines(target io.Writer, ui bool, options ViewOptions, plans []commandPlan) []string {
	lines := make([]string, 0, len(plans)*4)
	commandWidth := boxWidthFor(target) - visibleWidth("  ")
	if commandWidth < boxMinWidth {
		commandWidth = boxMinWidth
	}

	for index, plan := range plans {
		lines = append(lines, fmt.Sprintf("%s %s", stepBadge(ui, index+1), plan.Purpose))
		lines = append(lines, renderCommandBox(ui, plan.Command, commandWidth)...)
		if options.Verbose {
			lines = append(lines,
				fmt.Sprintf("%s %s", metaLabel(ui, "risk"), riskBadge(ui, plan.Risk)),
				fmt.Sprintf("%s %s", metaLabel(ui, "safety"), classificationBadge(ui, plan.Classification)),
				fmt.Sprintf("%s %s", metaLabel(ui, "confirm"), confirmBadge(ui, plan.RequiresConfirmation)),
			)
		}
		lines = append(lines, "")
	}
	return lines
}

// buildConfirmRow builds the styled "• confirm <question> <options>: <answer>" row.
// Pass an empty answer when rendering the initial prompt before input is collected.
func buildConfirmRow(ui bool, question, options, answer string) string {
	row := style(ui, colorYellow+colorBold, "• confirm ") +
		style(ui, colorWhite, question) +
		style(ui, colorWhite, " ") +
		options +
		style(ui, colorWhite, ": ")
	if answer != "" {
		row += style(ui, colorYellow+colorBold, answer)
	}
	return row
}

// promptPlanExecution asks whether the already rendered plan should be executed.
func promptPlanExecution(target io.Writer, ui bool, stdin *os.File) (bool, error) {
	box := newStepBox(target, ui, "confirm")
	box.Spacer()
	renderPlanExecutionPrompt(box)
	defer box.Close()

	key, ok, err := readSingleConfirmationKey(stdin)
	if err != nil {
		return false, err
	}
	if ok {
		if choice, found := parseConfirmationChoice(string(key)); found {
			run := choice == confirmationDefaultYes
			logPlanExecutionChoice(box, run)
			return run, nil
		}
	}

	line, err := readConfirmationLine(stdin)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))

	choice, ok := parseConfirmationChoice(answer)
	run := ok && choice == confirmationDefaultYes
	logPlanExecutionChoice(box, run)
	return run, nil
}

// renderPlanExecutionPrompt prints the plan-level confirmation question.
func renderPlanExecutionPrompt(box *stepBox) {
	if box == nil {
		return
	}
	box.writeRow(buildConfirmRow(box.ui, "Execute this plan?", renderPlanExecutionOptions(box.ui), ""))
}

// logPlanExecutionChoice records the plan-level confirmation decision.
func logPlanExecutionChoice(box *stepBox, run bool) {
	if box == nil {
		return
	}
	choice := "no"
	if run {
		choice = "yes"
	}
	box.ReplaceLastRenderedRow(buildConfirmRow(box.ui, "Execute this plan?", renderPlanExecutionOptions(box.ui), choice))
}

// renderPlanExecutionOptions renders the accepted plan confirmation keys.
func renderPlanExecutionOptions(ui bool) string {
	return "[" + style(ui, colorWhite, "y") + "/" + style(ui, colorWhite, "n") + "]"
}

// promptPlanningLimitContinuation asks whether Shellia should keep planning after the current round limit.
func promptPlanningLimitContinuation(target io.Writer, ui bool, stdin *os.File, limit int) (bool, error) {
	printWarningTo(target, ui, planningRoundLimitMessage(limit))

	box := newStepBox(target, ui, "confirm")
	box.Spacer()
	renderPlanningLimitPrompt(box, limit)
	defer box.Close()

	key, ok, err := readSingleConfirmationKey(stdin)
	if err != nil {
		return false, err
	}
	if ok {
		if choice, found := parseConfirmationChoice(string(key)); found {
			run := choice == confirmationDefaultYes
			logPlanningLimitChoice(box, limit, run)
			return run, nil
		}
	}

	line, err := readConfirmationLine(stdin)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))

	choice, ok := parseConfirmationChoice(answer)
	run := ok && choice == confirmationDefaultYes
	logPlanningLimitChoice(box, limit, run)
	return run, nil
}

// planningRoundLimitMessage explains the current planning limit and how to raise it.
func planningRoundLimitMessage(limit int) string {
	return fmt.Sprintf(
		"planning reached the current follow-up round limit (%d). Increase [execution] planning_max_rounds in config.toml or set SHELLIA_PLANNING_MAX_ROUNDS to allow more automatic rounds.",
		limit,
	)
}

// renderPlanningLimitPrompt prints the planning-limit continuation question.
func renderPlanningLimitPrompt(box *stepBox, limit int) {
	if box == nil {
		return
	}
	question := fmt.Sprintf("Reached planning round limit %d. Continue planning?", limit)
	box.surface.renderEditableRow(buildConfirmRow(box.ui, question, renderPlanExecutionOptions(box.ui), ""), 0)
}

// logPlanningLimitChoice records the planning-limit continuation decision.
func logPlanningLimitChoice(box *stepBox, limit int, run bool) {
	if box == nil {
		return
	}
	choice := "no"
	if run {
		choice = "yes"
	}
	question := fmt.Sprintf("Reached planning round limit %d. Continue planning?", limit)
	box.ReplaceLastRenderedRow(buildConfirmRow(box.ui, question, renderPlanExecutionOptions(box.ui), choice))
}

// printHeader shows a compact header with the global session state.
func printHeader(ui bool, options ViewOptions, ctxInfo contextInfo) {
	printHeaderTo(os.Stdout, ui, options, ctxInfo)
}

// printHeaderTo shows a compact header with the global session state on the provided target.
func printHeaderTo(target io.Writer, ui bool, options ViewOptions, ctxInfo contextInfo) {
	fmt.Fprintln(target)
	fmt.Fprintln(target, shelliaBrand(ui, false)+style(ui, colorDim, " · ")+shelliaVersionBadge(ui))
	if headerContext := plainHeaderContextValue(options, ctxInfo); headerContext != "" {
		fmt.Fprintln(target, style(ui, colorDim, headerContext))
	}
}

// printSection draws a section header with stronger visual hierarchy.
func printSection(ui bool, title string, color string) {
	printSectionTo(os.Stdout, ui, title, color)
}

// printSectionTo draws a section header with stronger visual hierarchy on the provided target.
func printSectionTo(target io.Writer, ui bool, title string, color string) {
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ui, color+colorBold, title))
}

// printRawPromptsTo prints the exact prompt messages sent to the model.
func printRawPromptsTo(target io.Writer, ui bool, title string, systemPrompt string, userPrompt string) {
	printSectionTo(target, ui, title, colorBlue)
	fmt.Fprintln(target, "system:")
	fmt.Fprintln(target, systemPrompt)
	fmt.Fprintln(target)
	fmt.Fprintln(target, "user:")
	fmt.Fprintln(target, userPrompt)
	fmt.Fprintln(target)
}

// printCommandExecution presents the active command inside a step box before running it.
func printCommandExecution(ui bool, options ViewOptions, index int, total int, plan commandPlan) *stepBox {
	return printCommandExecutionTo(os.Stdout, ui, options, index, total, plan)
}

// printCommandExecutionTo presents the active command inside a step box on the provided target.
func printCommandExecutionTo(target io.Writer, ui bool, options ViewOptions, index int, total int, plan commandPlan) *stepBox {
	box := newStepBox(target, ui, fmt.Sprintf("step %d/%d", index, total))
	box.Spacer()
	box.Command(plan.Command)
	box.Spacer()
	box.Bullet(plan.Purpose)
	if plan.Interactive {
		box.KeyValue("interactive", fallbackValue(plan.InteractiveReason, "yes"), colorYellow, colorWhite)
	}
	if options.Verbose {
		box.KeyValue("risk", plainRiskLabel(plan.Risk), colorYellow, colorWhite)
	}
	return box
}

// printInfo shows a short informational message.
func printInfo(ui bool, message string) {
	printInfoTo(os.Stdout, ui, message)
}

// printInfoTo shows a short informational message on the provided target.
func printInfoTo(target io.Writer, ui bool, message string) {
	fmt.Fprintf(target, "%s %s\n", shelliaBrand(ui, false), style(ui, colorWhite+colorBold, message))
}

// printInteractiveCommandStartTo shows the handoff message before an interactive command starts.
func printInteractiveCommandStartTo(target io.Writer, ui bool) {
	fmt.Fprintln(target)
	printInfoTo(target, ui, "Starting interactive command. Shellia will resume when it exits.")
}

// printModeStatus shows an interactive mode state change with cleaner output.
func printModeStatus(ui bool, message string) {
	printModeStatusTo(os.Stdout, ui, message)
}

// printModeStatusTo shows an interactive mode state change on the provided target.
func printModeStatusTo(target io.Writer, ui bool, message string) {
	renderPanel(target, ui, "mode", colorCyan, []string{
		style(ui, colorWhite+colorBold, message),
	})
}

// printNewSessionSeparatorTo marks a fresh context boundary without clearing the terminal.
func printNewSessionSeparatorTo(target io.Writer, ui bool) {
	fmt.Fprintln(target)
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ui, colorDim, centeredSeparatorText("new session · context cleared", boxWidthFor(target))))
	fmt.Fprintln(target)
}

// centeredSeparatorText renders a separator with a centered label.
func centeredSeparatorText(label string, width int) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return strings.Repeat("─", width)
	}

	text := " " + label + " "
	if width <= visibleWidth(text) {
		return text
	}

	remaining := width - visibleWidth(text)
	left := remaining / 2
	right := remaining - left
	return strings.Repeat("─", left) + text + strings.Repeat("─", right)
}

// printModelSwitchTo shows the active model profile after a /model change.
func printModelSwitchTo(target io.Writer, ui bool, options ViewOptions) {
	detail := ""
	if strings.TrimSpace(options.Model) != "" {
		detail = style(ui, colorDim, " · "+options.Model)
	}
	fmt.Fprintf(target, "\n%s %s%s\n", shelliaBrand(ui, false), style(ui, colorWhite+colorBold, "Model switched to "+options.ModelName+"."), detail)
}

// printWarning shows a non-fatal warning.
func printWarning(ui bool, message string) {
	printWarningTo(os.Stderr, ui, message)
}

// printWarningTo shows a non-fatal warning on the provided target.
func printWarningTo(target io.Writer, ui bool, message string) {
	fmt.Fprintf(target, "\n%s %s\n", style(ui, colorYellow+colorBold, "warning"), style(ui, colorWhite+colorBold, message))
}

// printSeparator shows the standard horizontal separator used between sections.
func printSeparator(target io.Writer, ui bool) {
	fmt.Fprintln(target, style(ui, colorDim, strings.Repeat("─", boxWidthFor(target))))
}

// printFinalResult shows the final useful answer for the user (non-streaming fallback).
func printFinalResult(ui bool, message string) {
	printFinalResultTo(os.Stdout, ui, message)
}

// printFinalResultTo shows the final useful answer on the provided target.
func printFinalResultTo(target io.Writer, ui bool, message string) {
	fmt.Fprintln(target)
	fmt.Fprintln(target, shelliaBrand(ui, false))
	renderAnswerBlock(target, ui, message)
	fmt.Fprintln(target)
	printSeparator(target, ui)
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
func renderAnswerBlock(target io.Writer, ui bool, message string) error {
	lines := renderAnswerMarkdown(message, answerContentWidth(ui), ui)
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
		if _, err := fmt.Fprint(target, prefix, line); err != nil {
			return err
		}
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
func readInteractivePrompt(ui bool, reader *bufio.Reader, stdin *os.File, stdout io.Writer, mode interactiveMode, options ViewOptions) (string, error) {
	return readInteractivePromptWithRenderer(ui, reader, stdin, stdout, mode, options, nil)
}

// readInteractivePromptWithRenderer renders submitted prompts through the selected presentation.
func readInteractivePromptWithRenderer(ui bool, reader *bufio.Reader, stdin *os.File, stdout io.Writer, mode interactiveMode, options ViewOptions, renderer *Renderer) (string, error) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, promptQuestionLine(ui, mode))
	prompt := renderer.interactivePromptPrefix(ui, mode)

	fd := int(stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprint(stdout, prompt)
		return readFallbackPromptLine(reader)
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprint(stdout, prompt)
		return readFallbackPromptLine(reader)
	}
	defer term.Restore(fd, state) //nolint:errcheck // best-effort terminal restore before returning to normal input.
	fmt.Fprint(stdout, "\x1b[?2004h")
	defer fmt.Fprint(stdout, "\x1b[?2004l")

	buffer := make([]rune, 0, 128)
	cursor := 0
	affinity := cursorAffinityForward
	single := []byte{0}
	renderState := &editableRenderState{}

	promptWidth := visibleWidth(prompt)
	renderWidth := promptRenderWidthFor(stdout)
	contentWidth := renderWidth - promptWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	renderEditablePromptTo(stdout, ui, prompt, buffer, cursor, affinity, renderState, options)

	for {
		_, err := stdin.Read(single)
		if err != nil {
			return "", fmt.Errorf("cannot read interactive prompt input: %w", err)
		}

		switch single[0] {
		case '\r', '\n':
			if !promptHasText(buffer) {
				if len(buffer) > 0 {
					buffer = buffer[:0]
					cursor = 0
					affinity = cursorAffinityForward
					renderEditablePromptTo(stdout, ui, prompt, buffer, cursor, affinity, renderState, options)
				}
				continue
			}
			submitted := strings.TrimSpace(string(buffer))
			clearSubmittedPromptTo(stdout, renderState, mode, renderer)
			renderSubmittedPrompt(stdout, ui, prompt, buffer, mode, renderer)
			return submitted, nil
		case 3:
			clearSubmittedPromptTo(stdout, renderState, mode, renderer)
			renderSubmittedPrompt(stdout, ui, prompt, buffer, mode, renderer)
			return "exit", nil
		case 27:
			result, err := applyPromptEscapeInput(stdin, fd, &buffer, &cursor, contentWidth, &affinity)
			if err != nil {
				return "", fmt.Errorf("cannot apply prompt escape sequence: %w", err)
			}
			if result.exit {
				clearSubmittedPromptTo(stdout, renderState, mode, renderer)
				renderSubmittedPrompt(stdout, ui, prompt, buffer, mode, renderer)
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
			if completed, ok := completeInteractiveCommand(string(buffer), options); ok {
				buffer = []rune(completed + " ")
				cursor = len(buffer)
				affinity = cursorAffinityForward
			}
		default:
			r, err := readInputRuneFrom(stdin, single[0])
			if err != nil {
				if errors.Is(err, errDiscardRune) {
					continue
				}
				return "", fmt.Errorf("cannot decode prompt input: %w", err)
			}
			insertPromptRunes(&buffer, &cursor, []rune{r})
			affinity = cursorAffinityForward
		}

		renderEditablePromptTo(stdout, ui, prompt, buffer, cursor, affinity, renderState, options)
	}
}

func renderSubmittedPrompt(target io.Writer, ui bool, prompt string, buffer []rune, mode interactiveMode, renderer *Renderer) {
	if renderer != nil && mode == interactiveModeAI {
		renderer.UserTurn(mode, string(buffer))
		return
	}
	printSubmittedPromptTo(target, ui, prompt, buffer)
	fmt.Fprint(target, "\r\n")
}

func clearSubmittedPromptTo(target io.Writer, state *editableRenderState, mode interactiveMode, renderer *Renderer) {
	clearEditablePromptTo(target, state)
	if renderer == nil || !renderer.ownsUserTurnQuestion(mode) {
		return
	}
	fmt.Fprint(target, "\033[1A\r\033[2K")
}

// readFallbackPromptLine reads one prompt line when raw terminal editing is unavailable.
func readFallbackPromptLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		if line == "" {
			return "", io.EOF
		}
		return strings.TrimSpace(line), nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read prompt line: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// promptHasText reports whether the editable prompt contains a real submission.
func promptHasText(buffer []rune) bool {
	return strings.TrimSpace(string(buffer)) != ""
}

// printSubmittedPrompt redraws only the submitted prompt and removes transient UI.
func printSubmittedPrompt(ui bool, prompt string, buffer []rune) {
	printSubmittedPromptTo(os.Stdout, ui, prompt, buffer)
}

// printSubmittedPromptTo redraws only the submitted prompt on the provided target.
func printSubmittedPromptTo(target io.Writer, ui bool, prompt string, buffer []rune) {
	lines, _, _ := editablePromptLayout(prompt, buffer, len(buffer), cursorAffinityForward, promptRenderWidthFor(target))
	promptWidth := visibleWidth(prompt)

	for index, line := range lines {
		if index > 0 {
			fmt.Fprint(target, "\r\n")
			fmt.Fprint(target, strings.Repeat(" ", promptWidth))
		} else {
			fmt.Fprint(target, prompt)
		}
		fmt.Fprint(target, style(ui, colorWhite, line))
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
	box.surface.renderEditableRow(buildConfirmRow(box.ui, prompt, renderConfirmationOptions(box.ui, defaultChoice), ""), 0)
}

// logConfirmationChoice records the confirmation decision on the same line as the original prompt.
func logConfirmationChoice(box *stepBox, prompt string, defaultChoice confirmationDefault, choice string) {
	if box == nil {
		return
	}
	fmt.Fprint(box.target, "\r\n")
	box.ReplaceLastRenderedRow(buildConfirmRow(box.ui, prompt, renderConfirmationOptions(box.ui, defaultChoice), choice))
}

// promptConfirmation asks for explicit confirmation inside the step box.
func promptConfirmation(box *stepBox, reader *bufio.Reader, stdin *os.File, prompt string, initialCommand string, defaultChoice confirmationDefault) (confirmDecision, string, error) {
	renderConfirmationPrompt(box, prompt, defaultChoice)

	key, ok, err := readSingleConfirmationKey(stdin)
	if err != nil {
		fmt.Fprint(box.target, "\r\n")
		return confirmDecisionCancel, "", err
	}
	if ok {
		for {
			if isConfirmationEnterKey(key) && defaultChoice != confirmationDefaultNone {
				return applyConfirmationChoice(box, reader, stdin, prompt, initialCommand, defaultChoice, defaultChoice)
			}

			lower := strings.ToLower(string(key))
			if choice, found := parseConfirmationChoice(lower); found {
				return applyConfirmationChoice(box, reader, stdin, prompt, initialCommand, defaultChoice, choice)
			}

			key, ok, err = readSingleConfirmationKey(stdin)
			if err != nil || !ok {
				break
			}
		}
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprint(box.target, "\r\n")
			return confirmDecisionCancel, "", fmt.Errorf("cannot read confirmation answer: %w", err)
		}

		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			if defaultChoice != confirmationDefaultNone {
				return applyConfirmationChoice(box, reader, stdin, prompt, initialCommand, defaultChoice, defaultChoice)
			}
			if errors.Is(err, io.EOF) {
				logConfirmationChoice(box, prompt, defaultChoice, "no")
				return confirmDecisionCancel, "", nil
			}
			continue
		}

		if choice, found := parseConfirmationChoice(answer); found {
			return applyConfirmationChoice(box, reader, stdin, prompt, initialCommand, defaultChoice, choice)
		}

		logConfirmationChoice(box, prompt, defaultChoice, "no")
		return confirmDecisionCancel, "", nil
	}
}

// readConfirmationLine reads exactly one answer so later prompts retain their input.
func readConfirmationLine(stdin *os.File) (string, error) {
	if stdin == nil {
		stdin = os.Stdin
	}

	var line strings.Builder
	buffer := []byte{0}
	for {
		_, err := stdin.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) && line.Len() > 0 {
				return line.String(), nil
			}
			return line.String(), err
		}
		line.WriteByte(buffer[0])
		if buffer[0] == '\n' {
			return line.String(), nil
		}
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
func applyConfirmationChoice(box *stepBox, reader *bufio.Reader, stdin *os.File, prompt string, initialCommand string, defaultChoice confirmationDefault, choice confirmationDefault) (confirmDecision, string, error) {
	switch choice {
	case confirmationDefaultYes:
		logConfirmationChoice(box, prompt, defaultChoice, "yes")
		return confirmDecisionRun, "", nil
	case confirmationDefaultEdit:
		logConfirmationChoice(box, prompt, defaultChoice, "edit")
		edited, editErr := box.EditCommand(reader, stdin, initialCommand)
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
func readSingleConfirmationKey(stdin *os.File) (byte, bool, error) {
	if stdin == nil {
		stdin = os.Stdin
	}

	fd := int(stdin.Fd())
	if !term.IsTerminal(fd) {
		return 0, false, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false, nil
	}
	defer term.Restore(fd, state) //nolint:errcheck // best-effort terminal restore before returning to normal input.

	buffer := []byte{0}
	_, err = stdin.Read(buffer)
	if err != nil {
		return 0, false, fmt.Errorf("cannot read confirmation key: %w", err)
	}
	if err := rawTurnPromptCancellation(buffer[0]); err != nil {
		return 0, false, err
	}

	return buffer[0], true, nil
}

// rawTurnPromptCancellation maps raw Ctrl+C input to turn cancellation.
func rawTurnPromptCancellation(key byte) error {
	if key == 3 {
		return context.Canceled
	}
	return nil
}

var errDiscardRune = errors.New("discard rune")

// readInputRune decodes a complete UTF-8 rune from the first byte already read.
func readInputRune(first byte) (rune, error) {
	return readInputRuneFrom(os.Stdin, first)
}

// readInputRuneFrom decodes a complete UTF-8 rune from the provided input reader.
func readInputRuneFrom(reader io.Reader, first byte) (rune, error) {
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
		if _, err := reader.Read(buf[index : index+1]); err != nil {
			return 0, fmt.Errorf("cannot read utf-8 continuation byte: %w", err)
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

// insertPromptRunes inserts text at the current prompt cursor and moves the
// cursor to the end of the inserted text.
func insertPromptRunes(buffer *[]rune, cursor *int, input []rune) {
	if len(input) == 0 {
		return
	}
	if *cursor < 0 {
		*cursor = 0
	}
	if *cursor > len(*buffer) {
		*cursor = len(*buffer)
	}
	*buffer = append((*buffer)[:*cursor], append(input, (*buffer)[*cursor:]...)...)
	*cursor += len(input)
}

type promptEscapeResult struct {
	exit bool
}

// applyPromptEscapeInput interprets Esc inside the main prompt editor.
func applyPromptEscapeInput(reader io.Reader, fd int, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (promptEscapeResult, error) {
	ready, err := isInputReady(fd)
	if err != nil {
		return promptEscapeResult{}, fmt.Errorf("cannot inspect pending terminal input: %w", err)
	}
	if !ready {
		return promptEscapeResult{exit: true}, nil
	}
	return applyPromptEscapeSequenceFrom(reader, buffer, cursor, contentWidth, affinity)
}

// applyPromptEscapeSequenceFrom handles the bytes following Esc in the main
// prompt editor, including Alt+Enter and bracketed paste.
func applyPromptEscapeSequenceFrom(reader io.Reader, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (promptEscapeResult, error) {
	next := []byte{0}
	if _, err := reader.Read(next); err != nil {
		return promptEscapeResult{}, fmt.Errorf("cannot read escape sequence byte: %w", err)
	}

	switch next[0] {
	case '\r', '\n':
		insertPromptRunes(buffer, cursor, []rune{'\n'})
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
		return promptEscapeResult{}, nil
	case '[':
		return applyPromptCSISequenceFrom(reader, buffer, cursor, contentWidth, affinity)
	default:
		return promptEscapeResult{}, nil
	}
}

func applyPromptCSISequenceFrom(reader io.Reader, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (promptEscapeResult, error) {
	command := []byte{0}
	if _, err := reader.Read(command); err != nil {
		return promptEscapeResult{}, fmt.Errorf("cannot read escape sequence command: %w", err)
	}

	switch command[0] {
	case '2':
		marker, err := readCSISequenceTail(reader)
		if err != nil {
			return promptEscapeResult{}, err
		}
		if marker == "200~" {
			pasted, err := readBracketedPaste(reader)
			if err != nil {
				return promptEscapeResult{}, err
			}
			insertPromptRunes(buffer, cursor, pasted)
			if affinity != nil {
				*affinity = cursorAffinityForward
			}
		}
	case '3':
		tilde := []byte{0}
		if _, err := reader.Read(tilde); err != nil {
			return promptEscapeResult{}, fmt.Errorf("cannot read delete escape terminator: %w", err)
		}
		if tilde[0] == '~' && *cursor < len(*buffer) {
			*buffer = append((*buffer)[:*cursor], (*buffer)[*cursor+1:]...)
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	default:
		applyPromptSimpleCSICommand(command[0], buffer, cursor, contentWidth, affinity)
	}

	return promptEscapeResult{}, nil
}

func applyPromptSimpleCSICommand(command byte, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) {
	switch command {
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
	}
}

func readCSISequenceTail(reader io.Reader) (string, error) {
	var builder strings.Builder
	builder.WriteByte('2')
	buf := []byte{0}
	for {
		if _, err := reader.Read(buf); err != nil {
			return "", fmt.Errorf("cannot read escape sequence marker: %w", err)
		}
		builder.WriteByte(buf[0])
		if buf[0] == '~' {
			return builder.String(), nil
		}
	}
}

// readBracketedPaste reads a terminal bracketed paste payload until its closing marker.
func readBracketedPaste(reader io.Reader) ([]rune, error) {
	terminator := []byte("\x1b[201~")
	payload := make([]byte, 0, 128)
	buf := []byte{0}

	for {
		if _, err := reader.Read(buf); err != nil {
			return nil, fmt.Errorf("cannot read bracketed paste: %w", err)
		}
		payload = append(payload, buf[0])
		if len(payload) >= len(terminator) && bytes.HasSuffix(payload, terminator) {
			payload = payload[:len(payload)-len(terminator)]
			return []rune(string(payload)), nil
		}
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
	for start <= len(buffer) {
		end := start
		for end < len(buffer) && buffer[end] != '\n' {
			end++
		}

		wrapPromptSegmentWithOffsets(buffer, start, end, width, &lines, &offsets)

		if end == len(buffer) {
			break
		}
		start = end + 1
	}

	if len(lines) == 0 {
		return []string{""}, []int{0}
	}
	return lines, offsets
}

func wrapPromptSegmentWithOffsets(buffer []rune, start int, end int, width int, lines *[]string, offsets *[]int) {
	if start == end {
		*lines = append(*lines, "")
		*offsets = append(*offsets, start)
		return
	}

	for start < end {
		*offsets = append(*offsets, start)
		remaining := end - start
		if remaining <= width {
			*lines = append(*lines, string(buffer[start:end]))
			return
		}

		lastSpace := -1
		for index := range width {
			if unicode.IsSpace(buffer[start+index]) {
				lastSpace = index
			}
		}

		chunkWidth := width
		if lastSpace > 0 {
			chunkWidth = lastSpace + 1
		}

		*lines = append(*lines, string(buffer[start:start+chunkWidth]))
		start += chunkWidth
	}
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
	return applyEscapeSequenceFrom(os.Stdin, buffer, cursor, contentWidth, affinity)
}

// applyEscapeSequenceFrom handles special cursor and editing keys from the provided input reader.
func applyEscapeSequenceFrom(reader io.Reader, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) error {
	sequence := []byte{0, 0}
	if _, err := reader.Read(sequence[:1]); err != nil {
		return fmt.Errorf("cannot read escape sequence prefix: %w", err)
	}
	if sequence[0] != '[' {
		return nil
	}
	if _, err := reader.Read(sequence[1:2]); err != nil {
		return fmt.Errorf("cannot read escape sequence command: %w", err)
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
		if _, err := reader.Read(tilde); err != nil {
			return fmt.Errorf("cannot read delete escape terminator: %w", err)
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
func applyEscapeSequenceOrExit(reader io.Reader, fd int, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (bool, error) {
	ready, err := isInputReady(fd)
	if err != nil {
		return false, fmt.Errorf("cannot inspect pending terminal input: %w", err)
	}
	if !ready {
		return true, nil
	}
	return false, applyEscapeSequenceFrom(reader, buffer, cursor, contentWidth, affinity)
}

// renderEditablePrompt repaints the full editable prompt block while handling wrapping correctly.
func renderEditablePrompt(ui bool, prompt string, buffer []rune, cursor int, affinity cursorAffinity, state *editableRenderState, options ViewOptions) {
	renderEditablePromptTo(os.Stdout, ui, prompt, buffer, cursor, affinity, state, options)
}

// renderEditablePromptTo repaints the full editable prompt block on the provided target.
func renderEditablePromptTo(target io.Writer, ui bool, prompt string, buffer []rune, cursor int, affinity cursorAffinity, state *editableRenderState, options ViewOptions) {
	lines, cursorRow, cursorCol := editablePromptLayout(prompt, buffer, cursor, affinity, promptRenderWidthFor(target))
	var menuLines []string
	if options.ShowCommandPopup {
		menuLines = commandMenuLines(ui, string(buffer), options)
	}
	promptWidth := visibleWidth(prompt)

	clearEditablePromptTo(target, state)

	for index, line := range lines {
		if index > 0 {
			fmt.Fprint(target, "\r\n")
		}
		if index == 0 {
			fmt.Fprint(target, prompt)
		} else {
			fmt.Fprint(target, strings.Repeat(" ", promptWidth))
		}
		fmt.Fprint(target, style(ui, colorWhite, line))
	}

	for _, line := range menuLines {
		fmt.Fprint(target, "\r\n")
		fmt.Fprint(target, line)
	}

	rows := len(lines) + len(menuLines)
	rowsBelow := rows - 1 - cursorRow
	fmt.Fprint(target, "\r")
	if rowsBelow > 0 {
		fmt.Fprintf(target, "\033[%dA", rowsBelow)
	}
	if cursorCol > 0 {
		fmt.Fprintf(target, "\033[%dC", cursorCol)
	}

	if state != nil {
		state.rows = rows
		state.cursorRow = cursorRow
	}
}

// clearEditablePrompt clears all rows from the previously rendered prompt.
func clearEditablePrompt(state *editableRenderState) {
	clearEditablePromptTo(os.Stdout, state)
}

// clearEditablePromptTo clears all rows from the previously rendered prompt on the provided target.
func clearEditablePromptTo(target io.Writer, state *editableRenderState) {
	if state == nil || state.rows == 0 {
		return
	}

	fmt.Fprint(target, "\r")
	if state.cursorRow > 0 {
		fmt.Fprintf(target, "\033[%dA", state.cursorRow)
	}
	for index := range state.rows {
		fmt.Fprint(target, "\033[2K")
		if index < state.rows-1 {
			fmt.Fprint(target, "\033[1B\r")
		}
	}
	if state.rows > 1 {
		fmt.Fprintf(target, "\033[%dA", state.rows-1)
	}
	fmt.Fprint(target, "\r")
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
	return promptRenderWidthFor(os.Stdout)
}

// promptRenderWidthFor returns the usable prompt width for the provided target.
func promptRenderWidthFor(target io.Writer) int {
	output, ok := target.(*os.File)
	if !ok {
		return 79
	}

	fd := int(output.Fd())
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
	clearScreenTo(os.Stdout)
}

// clearScreenTo clears the current terminal on the provided target.
func clearScreenTo(target io.Writer) {
	fmt.Fprint(target, "\033[2J\033[H")
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

// plainHeaderContextValue generates the short local context line for turn headers.
func plainHeaderContextValue(options ViewOptions, ctxInfo contextInfo) string {
	if !options.IncludeCWD {
		return ""
	}
	return ctxInfo.CWD
}

// plainHeaderModelValue generates a short model summary for the startup header.
func plainHeaderModelValue(options ViewOptions) string {
	return fallbackValue(options.Model, "-")
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
	return boxWidthFor(os.Stdout)
}

// boxWidthFor computes the total width of the step box using the provided target terminal size.
func boxWidthFor(target io.Writer) int {
	output, ok := target.(*os.File)
	if !ok {
		return 80
	}

	fd := int(output.Fd())
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
			for i < len(runes) && (runes[i] < '@' || runes[i] > '~') {
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

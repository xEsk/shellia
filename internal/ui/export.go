package ui

import (
	"bufio"
	"io"
	"os"
	"strings"
)

var version = "dev"

const (
	ColorDim    = colorDim
	ColorBlue   = colorBlue
	ColorCyan   = colorCyan
	ColorGreen  = colorGreen
	ColorYellow = colorYellow
)

// SetVersion records the binary version used by UI badges.
func SetVersion(value string) {
	version = strings.TrimSpace(value)
	if version == "" {
		version = "dev"
	}
}

// StepBox renders a full step with its state, confirmation, and output.
type StepBox = stepBox

// ConfirmDecision is the user's command confirmation decision.
type ConfirmDecision = confirmDecision

const (
	ConfirmDecisionCancel      = confirmDecisionCancel
	ConfirmDecisionRun         = confirmDecisionRun
	ConfirmDecisionEdit        = confirmDecisionEdit
	ConfirmDecisionInteractive = confirmDecisionInteractive
)

// Enabled reports whether styled terminal UI should be enabled.
func Enabled(cfg config) bool {
	return uiEnabled(cfg)
}

// PrintErrorTo renders a process-level error on the provided stream.
func PrintErrorTo(target io.Writer, ui bool, message string) {
	printErrorTo(target, ui, message)
}

// ExitWithError prints a process-level error and exits.
func ExitWithError(ui bool, message string, code int) {
	exitWithError(ui, message, code)
}

// RenderPanel draws a light visual block to better distinguish the Shellia UI.
func RenderPanel(target io.Writer, ui bool, title string, color string, lines []string) {
	renderPanel(target, ui, title, color, lines)
}

// PrintSessionBannerTo shows the polished startup banner on the provided target.
func PrintSessionBannerTo(target io.Writer, ui bool, cfg config) {
	printSessionBannerTo(target, ui, cfg)
}

// ReadInteractivePrompt reads one prompt from the interactive editor.
func ReadInteractivePrompt(ui bool, reader *bufio.Reader, stdin *os.File, stdout io.Writer, mode interactiveMode, cfg config) (string, error) {
	return readInteractivePrompt(ui, reader, stdin, stdout, mode, cfg)
}

// PrintWarningTo shows a non-fatal warning on the provided target.
func PrintWarningTo(target io.Writer, ui bool, message string) {
	printWarningTo(target, ui, message)
}

// PrintSeparator shows the standard horizontal separator used between sections.
func PrintSeparator(target io.Writer, ui bool) {
	printSeparator(target, ui)
}

// PrintInfoTo shows a short informational message on the provided target.
func PrintInfoTo(target io.Writer, ui bool, message string) {
	printInfoTo(target, ui, message)
}

// PrintContextTo shows the detected context on the provided target.
func PrintContextTo(target io.Writer, ui bool, cfg config, ctxInfo contextInfo) {
	printContextTo(target, ui, cfg, ctxInfo)
}

// PrintModeStatusTo shows an interactive mode state change on the provided target.
func PrintModeStatusTo(target io.Writer, ui bool, message string) {
	printModeStatusTo(target, ui, message)
}

// PrintModelSwitchTo shows the active model profile after a /model change.
func PrintModelSwitchTo(target io.Writer, ui bool, cfg config) {
	printModelSwitchTo(target, ui, cfg)
}

// PrintNewSessionSeparatorTo marks a fresh context boundary without clearing the terminal.
func PrintNewSessionSeparatorTo(target io.Writer, ui bool) {
	printNewSessionSeparatorTo(target, ui)
}

// ClearScreenTo clears the terminal using the provided target.
func ClearScreenTo(target io.Writer) {
	clearScreenTo(target)
}

// PrintHeaderTo shows a compact header with the global session state on the provided target.
func PrintHeaderTo(target io.Writer, ui bool, cfg config, ctxInfo contextInfo) {
	printHeaderTo(target, ui, cfg, ctxInfo)
}

// PrintFinalResultTo shows the final useful answer on the provided target.
func PrintFinalResultTo(target io.Writer, ui bool, message string) {
	printFinalResultTo(target, ui, message)
}

// PrintPlanOnlyGuidanceTo explains how to continue when a plan cannot be fully resolved yet.
func PrintPlanOnlyGuidanceTo(target io.Writer, ui bool, response llmResponse, confirmPlanOnly bool) {
	printPlanOnlyGuidanceTo(target, ui, response, confirmPlanOnly)
}

// PrintPlanTo presents the summary and commands proposed by the model on the provided target.
func PrintPlanTo(target io.Writer, ui bool, cfg config, summary string, plans []commandPlan, discovery bool) {
	printPlanTo(target, ui, cfg, summary, plans, discovery)
}

// PromptPlanExecution asks whether the plan should be executed.
func PromptPlanExecution(target io.Writer, ui bool, stdin *os.File) (bool, error) {
	return promptPlanExecution(target, ui, stdin)
}

// PrintRawPromptsTo prints the exact prompt messages sent to the model.
func PrintRawPromptsTo(target io.Writer, ui bool, title string, systemPrompt string, userPrompt string) {
	printRawPromptsTo(target, ui, title, systemPrompt, userPrompt)
}

// PrintSectionTo draws a section header with stronger visual hierarchy on the provided target.
func PrintSectionTo(target io.Writer, ui bool, title string, color string) {
	printSectionTo(target, ui, title, color)
}

// PromptPlanningLimitContinuation asks whether planning should continue after the configured limit.
func PromptPlanningLimitContinuation(target io.Writer, ui bool, stdin *os.File, limit int) (bool, error) {
	return promptPlanningLimitContinuation(target, ui, stdin, limit)
}

// OpenResultPanelTo opens the streamed result panel on the provided target.
func OpenResultPanelTo(target io.Writer, ui bool) {
	openResultPanelTo(target, ui)
}

// CloseResultPanelTo closes the streamed result panel on the provided target.
func CloseResultPanelTo(target io.Writer, ui bool) {
	closeResultPanelTo(target, ui)
}

// RenderAnswerBlock renders the Shellia answer with consistent wrapping.
func RenderAnswerBlock(target io.Writer, ui bool, message string, state *answerRenderState) error {
	return renderAnswerBlock(target, ui, message, state)
}

// ThinkingIndicator tracks an active thinking animation.
type ThinkingIndicator = thinkingIndicator

// StartThinkingIndicator starts the thinking animation when UI is enabled.
func StartThinkingIndicator(ui bool, target io.Writer) *ThinkingIndicator {
	return startThinkingIndicator(ui, target)
}

// Stop stops the thinking animation.
func (indicator *thinkingIndicator) Stop() {
	if indicator != nil {
		indicator.stop()
	}
}

// ResultWriter streams summary content into the result panel.
type ResultWriter = resultWriter

// NewResultWriter creates a result writer with a started thinking indicator.
func NewResultWriter(ui bool, target io.Writer) *ResultWriter {
	return &resultWriter{ui: ui, target: target, thinking: startThinkingIndicator(ui, target)}
}

// StopThinking stops the writer's thinking indicator.
func (writer *resultWriter) StopThinking() {
	writer.stopThinking()
}

// WroteAnything reports whether any streamed content reached the terminal.
func (writer *resultWriter) WroteAnything() bool {
	return writer != nil && writer.wroteAnything
}

// MarkWroteAnything marks the writer as having rendered content.
func (writer *resultWriter) MarkWroteAnything() {
	if writer != nil {
		writer.wroteAnything = true
	}
}

// AnswerState returns the writer's render state.
func (writer *resultWriter) AnswerState() *answerRenderState {
	if writer == nil {
		return nil
	}
	return &writer.state
}

// PlanOnlyResult builds the stored result for plan-only turns.
func PlanOnlyResult(summary string, response llmResponse) string {
	return planOnlyResult(summary, response)
}

// PrintCommandExecutionTo presents the active command inside a step box.
func PrintCommandExecutionTo(target io.Writer, ui bool, cfg config, index int, total int, plan commandPlan) *StepBox {
	return printCommandExecutionTo(target, ui, cfg, index, total, plan)
}

// PrintInteractiveCommandStartTo shows the handoff message before an interactive command starts.
func PrintInteractiveCommandStartTo(target io.Writer, ui bool) {
	printInteractiveCommandStartTo(target, ui)
}

// PromptConfirmation asks for explicit confirmation inside the step box.
func PromptConfirmation(box *StepBox, reader *bufio.Reader, stdin *os.File, prompt string, initialCommand string, defaultChoice confirmationDefault) (ConfirmDecision, string, error) {
	return promptConfirmation(box, reader, stdin, prompt, initialCommand, defaultChoice)
}

// TraceConfirmationDecision converts a confirmation decision to trace text.
func TraceConfirmationDecision(decision ConfirmDecision) string {
	switch decision {
	case confirmDecisionRun:
		return "run"
	case confirmDecisionEdit:
		return "edit"
	case confirmDecisionInteractive:
		return "interactive"
	case confirmDecisionCancel:
		return "cancel"
	default:
		return "unknown"
	}
}

// StyleStart returns the ANSI prefix for a colour when UI is enabled.
func StyleStart(ui bool, color string) string {
	return styleStart(ui, color)
}

// StyleEnd returns the ANSI reset suffix when UI is enabled.
func StyleEnd(ui bool) string {
	return styleEnd(ui)
}

// NewStepBox creates a step box.
func NewStepBox(target io.Writer, ui bool, title string) *StepBox {
	return newStepBox(target, ui, title)
}

// IsClosed reports whether the step box has already been closed.
func (box *stepBox) IsClosed() bool {
	return box == nil || box.closed
}

package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

const (
	cardsUserBackground      = "\033[48;2;17;49;58m"
	cardsShelliaBackground   = "\033[48;2;64;45;70m"
	cardsExecutionBackground = "\033[48;2;35;39;41m"
)

type cardsRenderer struct {
	target io.Writer
	ansi   bool
	user   string
}

type cardsTurn struct {
	target       io.Writer
	ansi         bool
	width        int
	open         bool
	lastRowBlank bool
	suspended    bool
	closed       bool
	activeStep   *cardsStepSurface
}

type cardsStepSurface struct {
	turn      *cardsTurn
	title     string
	width     int
	open      bool
	suspended bool
	closed    bool
}

func newCardsRenderer(target io.Writer, ansi bool) rendererImpl {
	return newCardsRendererWithUser(target, ansi, "")
}

func newCardsRendererWithUser(target io.Writer, ansi bool, user string) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &cardsRenderer{target: target, ansi: ansi, user: strings.TrimSpace(user)}
}

func (renderer *cardsRenderer) ownsUserTurnQuestion() bool {
	return true
}

func (renderer *cardsRenderer) interactivePromptPrefix(mode core.InteractiveMode) string {
	if mode != core.InteractiveModeAI {
		return promptPrefix(renderer.ansi, mode)
	}
	user := fallbackValue(renderer.user, "you")
	return style(renderer.ansi, colorCyan+colorBold, user) + style(renderer.ansi, colorWhite, " › ")
}

func (renderer *cardsRenderer) userTurn(mode core.InteractiveMode, text string) {
	if renderer == nil {
		return
	}

	width := boxWidthFor(renderer.target)
	user := fallbackValue(renderer.user, "you")
	cardsWriteBlank(renderer.target)
	cardsWriteLine(renderer.target, cardsBorderLine(renderer.ansi, colorCyan, cardsUserBackground, true, style(renderer.ansi, colorCyan+colorBold, user), width, true))
	row, _ := cardsFormattedRow(renderer.ansi, colorCyan, cardsUserBackground, width, "")
	cardsWriteLine(renderer.target, row)
	cardsWriteSubmittedRows(
		renderer.target,
		renderer.ansi,
		colorCyan,
		cardsUserBackground,
		width,
		text,
	)
	row, _ = cardsFormattedRow(renderer.ansi, colorCyan, cardsUserBackground, width, "")
	cardsWriteLine(renderer.target, row)
	cardsWriteLine(renderer.target, cardsBorderLine(renderer.ansi, colorCyan, cardsUserBackground, true, "", width, false))
}

func (renderer *cardsRenderer) beginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) turnImpl {
	if renderer == nil {
		return &cardsTurn{closed: true}
	}

	turn := &cardsTurn{
		target: renderer.target,
		ansi:   renderer.ansi,
		width:  boxWidthFor(renderer.target),
	}
	turn.openCard(shelliaBrand(turn.ansi, false) + style(turn.ansi, colorDim, " · ") + shelliaVersionBadge(turn.ansi))
	turn.writeRow("")
	if cfg.IncludeCWD && strings.TrimSpace(ctxInfo.CWD) != "" {
		turn.writeRow("  " + style(turn.ansi, colorDim, ctxInfo.CWD))
	}
	return turn
}

func (turn *cardsTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if !turn.canWrite() {
		return
	}
	turn.ensurePaddingRow()

	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	turn.writeRow("  " + style(turn.ansi, titleColor+colorBold, title))
	turn.writeWrapped("    ", style(turn.ansi, colorDim, "    "), summary)

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		return
	}
	turn.writeRow("")
	turn.writeRow("  " + style(turn.ansi, colorDim+colorBold, "steps"))
	for index, plan := range plans {
		purposePrefix := fmt.Sprintf("    %d. ", index+1)
		turn.writeWrapped(purposePrefix, style(turn.ansi, colorDim, purposePrefix), plan.Purpose)
		turn.writeWrapped("    run › ", style(turn.ansi, colorCyan+colorBold, "    run › "), plan.Command)
		if cfg.Verbose {
			turn.writeWrapped("    ", style(turn.ansi, colorDim, "    "), fmt.Sprintf("risk %s", plainRiskLabel(plan.Risk)))
		}
	}
}

func (turn *cardsTurn) beginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *stepBox {
	if !turn.canWrite() {
		return nil
	}
	if turn.activeStep != nil {
		turn.activeStep.close()
	}

	turn.writeRow("")
	title := fmt.Sprintf("step %d/%d", index, total)
	surface := &cardsStepSurface{
		turn:  turn,
		title: title,
		width: cardsContentWidth(turn.width) - 4,
	}
	if surface.width < 4 {
		surface.width = 4
	}
	turn.activeStep = surface
	surface.openSurface(title)
	box := newStepBoxForSurface(surface)
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

func (turn *cardsTurn) final(message string) {
	if !turn.canWrite() {
		return
	}
	if turn.activeStep != nil {
		turn.activeStep.close()
	}

	turn.ensurePaddingRow()
	turn.writeRow("  " + shelliaBrand(turn.ansi, false))
	lines := renderAnswerMarkdown(message, cardsContentWidth(turn.width)-4, turn.ansi)
	for _, line := range lines {
		turn.writeRow("    " + line)
	}
	turn.writeRow("")
}

func (turn *cardsTurn) thinkingPrefix() string {
	if !turn.canWrite() {
		return ""
	}
	turn.ensurePaddingRow()
	if !turn.ansi {
		return "│   "
	}
	return cardsShelliaBackground + colorMagenta + "│ " + colorReset + cardsShelliaBackground + "  "
}

func (turn *cardsTurn) suspend() {
	if turn == nil || turn.closed || turn.suspended {
		return
	}
	if turn.activeStep != nil {
		turn.activeStep.suspendSurface()
	}
	turn.closeCard()
	turn.suspended = true
}

func (turn *cardsTurn) resume() {
	if turn == nil || turn.closed || !turn.suspended {
		return
	}
	turn.openCard("Shellia · continued")
	turn.suspended = false
	if turn.activeStep != nil {
		turn.activeStep.resumeSurface()
	}
}

func (turn *cardsTurn) close() {
	if turn == nil || turn.closed {
		return
	}
	if turn.activeStep != nil {
		turn.activeStep.close()
	}
	turn.closeCard()
	turn.suspended = false
	turn.closed = true
}

func (turn *cardsTurn) canWrite() bool {
	return turn != nil && turn.open && !turn.suspended && !turn.closed
}

func (turn *cardsTurn) openCard(label string) {
	if turn == nil || turn.closed || turn.open {
		return
	}
	cardsWriteBlank(turn.target)
	cardsWriteLine(turn.target, cardsBorderLine(turn.ansi, colorMagenta, cardsShelliaBackground, true, label, turn.width, true))
	turn.open = true
	turn.lastRowBlank = false
}

func (turn *cardsTurn) closeCard() {
	if turn == nil || !turn.open {
		return
	}
	cardsWriteLine(turn.target, cardsBorderLine(turn.ansi, colorMagenta, cardsShelliaBackground, true, "", turn.width, false))
	turn.open = false
}

func (turn *cardsTurn) writeRow(rendered string) {
	if !turn.canWrite() {
		return
	}
	row, _ := cardsFormattedRow(turn.ansi, colorMagenta, cardsShelliaBackground, turn.width, rendered)
	cardsWriteLine(turn.target, row)
	turn.lastRowBlank = rendered == ""
}

func (turn *cardsTurn) writeWrapped(prefixPlain string, prefixRendered string, text string) {
	if !turn.canWrite() {
		return
	}
	cardsWriteWrappedRow(turn.target, turn.ansi, colorMagenta, cardsShelliaBackground, turn.width, prefixRendered, prefixPlain, text)
	turn.lastRowBlank = false
}

func (turn *cardsTurn) ensurePaddingRow() {
	if !turn.canWrite() || turn.lastRowBlank {
		return
	}
	turn.writeRow("")
}

func (surface *cardsStepSurface) writer() io.Writer {
	if surface == nil || surface.turn == nil {
		return io.Discard
	}
	return surface.turn.target
}

func (surface *cardsStepSurface) ansiEnabled() bool {
	return surface != nil && surface.turn != nil && surface.turn.ansi
}

func (surface *cardsStepSurface) contentWidth() int {
	if surface == nil {
		return 1
	}
	return cardsContentWidth(surface.width)
}

func (surface *cardsStepSurface) writeRow(rendered string) {
	if !surface.canWrite() {
		return
	}
	for _, row := range wrapRenderedRows(rendered, surface.contentWidth()) {
		nested, _ := cardsFormattedRow(surface.turn.ansi, colorDim, cardsExecutionBackground, surface.width, row)
		surface.turn.writeRow("  " + nested)
	}
}

func (surface *cardsStepSurface) replaceLastRenderedRow(rendered string) {
	if !surface.canWrite() {
		return
	}
	output, ok := surface.turn.target.(*os.File)
	if !ok || !term.IsTerminal(int(output.Fd())) {
		surface.writeRow(rendered)
		return
	}

	row, _ := surface.formattedRow(rendered)
	fmt.Fprint(surface.turn.target, "\033[1A\r\033[2K")
	cardsWriteLine(surface.turn.target, row)
}

func (surface *cardsStepSurface) renderEditableRow(rendered string, moveLeft int) {
	if !surface.canWrite() {
		return
	}
	row, trailing := surface.formattedRow(rendered)
	fmt.Fprint(surface.turn.target, "\r\033[K", row)
	moveLeft += trailing
	if moveLeft > 0 {
		fmt.Fprintf(surface.turn.target, "\033[%dD", moveLeft)
	}
}

func (surface *cardsStepSurface) close() {
	if surface == nil || surface.closed {
		return
	}
	if surface.open && surface.turn != nil && surface.turn.canWrite() {
		surface.turn.writeRow("  " + cardsBorderLine(surface.turn.ansi, colorDim, cardsExecutionBackground, false, "", surface.width, false))
	}
	surface.open = false
	surface.suspended = false
	surface.closed = true
	if surface.turn != nil && surface.turn.activeStep == surface {
		surface.turn.activeStep = nil
	}
}

func (surface *cardsStepSurface) canWrite() bool {
	return surface != nil && surface.open && !surface.suspended && !surface.closed && surface.turn != nil && surface.turn.canWrite()
}

func (surface *cardsStepSurface) openSurface(label string) {
	if surface == nil || surface.turn == nil || surface.closed || surface.open || !surface.turn.canWrite() {
		return
	}
	surface.turn.writeRow("  " + cardsBorderLine(surface.turn.ansi, colorDim, cardsExecutionBackground, false, style(surface.turn.ansi, colorBlue+colorBold, label), surface.width, true))
	surface.open = true
}

func (surface *cardsStepSurface) suspendSurface() {
	if !surface.canWrite() {
		return
	}
	surface.turn.writeRow("  " + cardsBorderLine(surface.turn.ansi, colorDim, cardsExecutionBackground, false, "", surface.width, false))
	surface.open = false
	surface.suspended = true
}

func (surface *cardsStepSurface) resumeSurface() {
	if surface == nil || surface.turn == nil || surface.closed || !surface.suspended || !surface.turn.canWrite() {
		return
	}
	surface.suspended = false
	surface.openSurface(surface.title + " · continued")
}

func (surface *cardsStepSurface) formattedRow(rendered string) (string, int) {
	nested, nestedTrailing := cardsFormattedRow(surface.turn.ansi, colorDim, cardsExecutionBackground, surface.width, rendered)
	row, outerTrailing := cardsFormattedRow(surface.turn.ansi, colorMagenta, cardsShelliaBackground, surface.turn.width, "  "+nested)
	return row, nestedTrailing + outerTrailing
}

func cardsWriteWrappedRow(target io.Writer, ansi bool, borderColor string, background string, width int, prefixRendered string, prefixPlain string, text string) {
	available := cardsContentWidth(width) - visibleWidth(prefixPlain)
	if available < 1 {
		available = 1
	}
	lines := wrapPlainText(text, available)
	for index, line := range lines {
		prefix := strings.Repeat(" ", visibleWidth(prefixPlain))
		if index == 0 {
			prefix = prefixRendered
		}
		row, _ := cardsFormattedRow(ansi, borderColor, background, width, prefix+line)
		cardsWriteLine(target, row)
	}
}

func cardsWriteSubmittedRows(target io.Writer, ansi bool, borderColor string, background string, width int, text string) {
	const indent = "  "
	available := cardsContentWidth(width) - visibleWidth(indent)
	if available < 1 {
		available = 1
	}
	lines := cardsWrapSubmittedText(text, available)
	for _, line := range lines {
		row, _ := cardsFormattedRow(ansi, borderColor, background, width, indent+style(ansi, colorWhite, line))
		cardsWriteLine(target, row)
	}
}

func cardsWrapSubmittedText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	physicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(physicalLines))
	for _, physicalLine := range physicalLines {
		runes := []rune(physicalLine)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > width {
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		lines = append(lines, string(runes))
	}
	return lines
}

func cardsFormattedRow(ansi bool, borderColor string, background string, width int, rendered string) (string, int) {
	padding := cardsContentWidth(width) - visibleWidth(rendered)
	if padding < 0 {
		padding = 0
	}
	row := style(ansi, borderColor, "│ ") + rendered + strings.Repeat(" ", padding) + style(ansi, borderColor, " │")
	return cardsApplyBackground(ansi, background, row), padding + visibleWidth(" │")
}

func cardsBorderLine(ansi bool, borderColor string, background string, rounded bool, label string, width int, top bool) string {
	if width < 4 {
		width = 4
	}
	topLeft, topRight := "┌", "┐"
	bottomLeft, bottomRight := "└", "┘"
	if rounded {
		topLeft, topRight = "╭", "╮"
		bottomLeft, bottomRight = "╰", "╯"
		background = ""
	}
	if !top {
		return cardsApplyBackground(ansi, background, style(ansi, borderColor, bottomLeft+strings.Repeat("─", width-2)+bottomRight))
	}

	prefix := topLeft + "─ "
	remaining := width - visibleWidth(prefix) - visibleWidth(label) - visibleWidth(" "+topRight)
	if remaining < 0 {
		remaining = 0
	}
	row := style(ansi, borderColor, prefix) + label + style(ansi, borderColor, " "+strings.Repeat("─", remaining)+topRight)
	return cardsApplyBackground(ansi, background, row)
}

func cardsContentWidth(width int) int {
	width -= visibleWidth("│ ") + visibleWidth(" │")
	if width < 1 {
		return 1
	}
	return width
}

func cardsApplyBackground(ansi bool, background string, row string) string {
	if !ansi || background == "" {
		return row
	}
	row = strings.NewReplacer(
		colorReset, colorReset+background,
		"\033[m", "\033[m"+background,
	).Replace(row)
	return background + row + colorReset
}

func cardsWriteLine(target io.Writer, row string) {
	fmt.Fprint(target, row, "\r\n")
}

func cardsWriteBlank(target io.Writer) {
	fmt.Fprint(target, "\r\n")
}

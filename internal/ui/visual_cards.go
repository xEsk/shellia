package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

type cardsRenderer struct {
	target io.Writer
	ansi   bool
}

type cardsTurn struct {
	target     io.Writer
	ansi       bool
	width      int
	open       bool
	suspended  bool
	closed     bool
	activeStep *cardsStepSurface
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
	if target == nil {
		target = io.Discard
	}
	return &cardsRenderer{target: target, ansi: ansi}
}

func (renderer *cardsRenderer) userTurn(mode core.InteractiveMode, text string) {
	if renderer == nil {
		return
	}

	width := boxWidthFor(renderer.target)
	fmt.Fprintln(renderer.target)
	fmt.Fprintln(renderer.target, cardsBorderLine(renderer.ansi, colorCyan, "Tu", width, true))
	cardsWriteSubmittedRows(
		renderer.target,
		renderer.ansi,
		colorCyan,
		width,
		promptPrefix(renderer.ansi, mode),
		promptPrefix(false, mode),
		text,
	)
	fmt.Fprintln(renderer.target, cardsBorderLine(renderer.ansi, colorCyan, "", width, false))
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
	turn.openCard("Shellia")
	turn.writeRow(shelliaBrand(turn.ansi, false) + style(turn.ansi, colorDim, " · ") + shelliaVersionBadge(turn.ansi))
	if cfg.IncludeCWD && strings.TrimSpace(ctxInfo.CWD) != "" {
		turn.writeRow(style(turn.ansi, colorDim, ctxInfo.CWD))
	}
	return turn
}

func (turn *cardsTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if !turn.canWrite() {
		return
	}

	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	turn.writeRow("")
	turn.writeRow(style(turn.ansi, titleColor+colorBold, title))
	turn.writeWrapped("  ", style(turn.ansi, colorDim, "  "), summary)

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		return
	}
	turn.writeRow("")
	turn.writeRow(style(turn.ansi, colorDim+colorBold, "steps"))
	for index, plan := range plans {
		purposePrefix := fmt.Sprintf("  %d. ", index+1)
		turn.writeWrapped(purposePrefix, style(turn.ansi, colorDim, purposePrefix), plan.Purpose)
		turn.writeWrapped("  run › ", style(turn.ansi, colorCyan+colorBold, "  run › "), plan.Command)
		if cfg.Verbose {
			turn.writeWrapped("  ", style(turn.ansi, colorDim, "  "), fmt.Sprintf("risk %s", plainRiskLabel(plan.Risk)))
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
		width: cardsContentWidth(turn.width),
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

	turn.writeRow("")
	turn.writeRow(style(turn.ansi, colorMagenta+colorBold, "Shellia"))
	lines := renderAnswerMarkdown(message, cardsContentWidth(turn.width)-2, turn.ansi)
	for _, line := range lines {
		turn.writeRow("  " + line)
	}
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
	fmt.Fprintln(turn.target)
	fmt.Fprintln(turn.target, cardsBorderLine(turn.ansi, colorMagenta, label, turn.width, true))
	turn.open = true
}

func (turn *cardsTurn) closeCard() {
	if turn == nil || !turn.open {
		return
	}
	fmt.Fprintln(turn.target, cardsBorderLine(turn.ansi, colorMagenta, "", turn.width, false))
	turn.open = false
}

func (turn *cardsTurn) writeRow(rendered string) {
	if !turn.canWrite() {
		return
	}
	row, _ := cardsFormattedRow(turn.ansi, colorMagenta, turn.width, rendered)
	fmt.Fprintln(turn.target, row)
}

func (turn *cardsTurn) writeWrapped(prefixPlain string, prefixRendered string, text string) {
	if !turn.canWrite() {
		return
	}
	cardsWriteWrappedRow(turn.target, turn.ansi, colorMagenta, turn.width, prefixRendered, prefixPlain, text)
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
	for _, row := range cardsWrapRenderedRows(rendered, surface.contentWidth()) {
		nested, _ := cardsFormattedRow(surface.turn.ansi, colorDim, surface.width, row)
		surface.turn.writeRow(nested)
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
	fmt.Fprintln(surface.turn.target, row)
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
		surface.turn.writeRow(cardsBorderLine(surface.turn.ansi, colorDim, "", surface.width, false))
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
	surface.turn.writeRow(cardsBorderLine(surface.turn.ansi, colorDim, label, surface.width, true))
	surface.open = true
}

func (surface *cardsStepSurface) suspendSurface() {
	if !surface.canWrite() {
		return
	}
	surface.turn.writeRow(cardsBorderLine(surface.turn.ansi, colorDim, "", surface.width, false))
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
	nested, nestedTrailing := cardsFormattedRow(surface.turn.ansi, colorDim, surface.width, rendered)
	row, outerTrailing := cardsFormattedRow(surface.turn.ansi, colorMagenta, surface.turn.width, nested)
	return row, nestedTrailing + outerTrailing
}

func cardsWriteWrappedRow(target io.Writer, ansi bool, borderColor string, width int, prefixRendered string, prefixPlain string, text string) {
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
		row, _ := cardsFormattedRow(ansi, borderColor, width, prefix+line)
		fmt.Fprintln(target, row)
	}
}

func cardsWriteSubmittedRows(target io.Writer, ansi bool, borderColor string, width int, prefixRendered string, prefixPlain string, text string) {
	available := cardsContentWidth(width) - visibleWidth(prefixPlain)
	if available < 1 {
		available = 1
	}
	lines := cardsWrapSubmittedText(text, available)
	for index, line := range lines {
		prefix := strings.Repeat(" ", visibleWidth(prefixPlain))
		if index == 0 {
			prefix = prefixRendered
		}
		row, _ := cardsFormattedRow(ansi, borderColor, width, prefix+line)
		fmt.Fprintln(target, row)
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

func cardsWrapRenderedRows(rendered string, width int) []string {
	if width < 1 {
		width = 1
	}
	if visibleWidth(rendered) <= width {
		return []string{rendered}
	}

	lines := make([]string, 0, (visibleWidth(rendered)/width)+1)
	var current strings.Builder
	visible := 0
	activeStyle := ""
	for offset := 0; offset < len(rendered); {
		if sequence, size := cardsANSISequence(rendered[offset:]); size > 0 {
			current.WriteString(sequence)
			if strings.HasSuffix(sequence, "m") {
				if sequence == colorReset {
					activeStyle = ""
				} else {
					activeStyle += sequence
				}
			}
			offset += size
			continue
		}

		_, size := utf8.DecodeRuneInString(rendered[offset:])
		if size == 0 {
			break
		}
		if visible == width {
			if activeStyle != "" {
				current.WriteString(colorReset)
			}
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(activeStyle)
			visible = 0
		}
		current.WriteString(rendered[offset : offset+size])
		visible++
		offset += size
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func cardsANSISequence(text string) (string, int) {
	if len(text) < 3 || text[0] != '\033' || text[1] != '[' {
		return "", 0
	}
	for index := 2; index < len(text); index++ {
		if text[index] >= '@' && text[index] <= '~' {
			return text[:index+1], index + 1
		}
	}
	return "", 0
}

func cardsFormattedRow(ansi bool, borderColor string, width int, rendered string) (string, int) {
	padding := cardsContentWidth(width) - visibleWidth(rendered)
	if padding < 0 {
		padding = 0
	}
	row := style(ansi, borderColor, "│ ") + rendered + strings.Repeat(" ", padding) + style(ansi, borderColor, " │")
	return row, padding + visibleWidth(" │")
}

func cardsBorderLine(ansi bool, borderColor string, label string, width int, top bool) string {
	if width < 4 {
		width = 4
	}
	if !top {
		return style(ansi, borderColor, "└"+strings.Repeat("─", width-2)+"┘")
	}

	prefix := "┌─ " + label + " "
	remaining := width - visibleWidth(prefix) - visibleWidth("┐")
	if remaining < 0 {
		remaining = 0
	}
	return style(ansi, borderColor, prefix+strings.Repeat("─", remaining)+"┐")
}

func cardsContentWidth(width int) int {
	width -= visibleWidth("│ ") + visibleWidth(" │")
	if width < 1 {
		return 1
	}
	return width
}

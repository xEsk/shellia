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
	bandsMarker              = "▌"
	bandsUserBackground      = guideUserBackground
	bandsShelliaBackground   = "\033[48;2;64;45;70m"
	bandsExecutionBackground = "\033[48;2;35;39;41m"
)

type bandsRenderer struct {
	target io.Writer
	ansi   bool
	user   string
}

type bandsTurn struct {
	target       io.Writer
	ansi         bool
	closed       bool
	lastRowBlank bool
}

type bandsStepSurface struct {
	target     io.Writer
	ansi       bool
	totalWidth int
	width      int
	closed     bool
}

func newBandsRenderer(target io.Writer, ansi bool) rendererImpl {
	return newBandsRendererWithUser(target, ansi, "")
}

func newBandsRendererWithUser(target io.Writer, ansi bool, user string) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &bandsRenderer{target: target, ansi: ansi, user: strings.TrimSpace(user)}
}

func (renderer *bandsRenderer) ownsUserTurnQuestion() bool {
	return true
}

func (renderer *bandsRenderer) interactivePromptPrefix(mode core.InteractiveMode) string {
	if mode != core.InteractiveModeAI {
		return promptPrefix(renderer.ansi, mode)
	}
	user := fallbackValue(renderer.user, "you")
	return style(renderer.ansi, colorCyan+colorBold, user) + style(renderer.ansi, colorWhite, " › ")
}

func (renderer *bandsRenderer) userTurn(mode core.InteractiveMode, text string) {
	if renderer == nil {
		return
	}

	width := boxWidthFor(renderer.target)
	contentIndent := "  "
	userWidth := width - visibleWidth(bandsMarker+"  ") - 2
	if userWidth < 1 {
		userWidth = 1
	}
	messageWidth := width - visibleWidth(bandsMarker+"  "+contentIndent) - 2
	if messageWidth < 1 {
		messageWidth = 1
	}

	bandsWriteBlank(renderer.target)
	bandsWriteRow(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, "", width))
	user := fallbackValue(renderer.user, "you")
	for _, line := range wrapPromptRunes([]rune(user), userWidth) {
		bandsWriteRow(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, style(renderer.ansi, colorCyan+colorBold, line), width))
	}
	for _, line := range wrapPromptRunes([]rune(text), messageWidth) {
		bandsWriteRow(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, contentIndent+style(renderer.ansi, colorWhite, line), width))
	}
	bandsWriteRow(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, "", width))
}

func (renderer *bandsRenderer) beginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) turnImpl {
	if renderer == nil {
		return &bandsTurn{target: io.Discard}
	}

	turn := &bandsTurn{target: renderer.target, ansi: renderer.ansi}
	bandsWriteBlank(turn.target)
	turn.ensurePaddingRow()
	turn.row(shelliaBrand(turn.ansi, false) + style(turn.ansi, colorDim, " · "+fallbackValue(strings.TrimSpace(version), "dev")))
	if context := plainHeaderContextValue(cfg, ctxInfo); context != "" {
		turn.row(style(turn.ansi, colorDim, context))
	}
	turn.row("")
	return turn
}

func (turn *bandsTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if turn == nil || turn.closed {
		return
	}
	turn.ensurePaddingRow()

	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	turn.row(style(turn.ansi, titleColor+colorBold, title))
	for _, line := range wrapPlainText(summary, turn.contentWidth("  ")) {
		turn.row("  " + style(turn.ansi, colorWhite, line))
	}

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		turn.row("")
		return
	}

	turn.row("")
	turn.row(style(turn.ansi, colorDim+colorBold, "steps"))
	for _, line := range bandsPlanStepLines(turn.ansi, cfg, plans, turn.contentWidth("  ")) {
		turn.row("  " + line)
	}
	turn.row("")
}

func (turn *bandsTurn) beginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *stepBox {
	if turn == nil || turn.closed {
		return nil
	}

	turn.lastRowBlank = false
	surface := newBandsStepSurface(turn.target, turn.ansi)
	surface.writeRow(style(turn.ansi, colorDim+colorBold, fmt.Sprintf("step %d/%d", index, total)))
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

func (turn *bandsTurn) final(message string) {
	if turn == nil || turn.closed {
		return
	}

	turn.ensurePaddingRow()
	turn.row(shelliaBrand(turn.ansi, false))
	for _, line := range renderAnswerMarkdown(message, turn.contentWidth("  "), turn.ansi) {
		turn.row("  " + line)
	}
	turn.row("")
}

func (turn *bandsTurn) thinkingPrefix() string {
	if turn == nil || turn.closed {
		return ""
	}
	turn.ensurePaddingRow()
	if !turn.ansi {
		return bandsMarker + "  "
	}
	return bandsShelliaBackground + "\033[2K" + colorMagenta + bandsMarker + colorReset + bandsShelliaBackground + "  "
}

func (turn *bandsTurn) suspend() {}

func (turn *bandsTurn) resume() {}

func (turn *bandsTurn) close() {
	if turn == nil || turn.closed {
		return
	}
	turn.closed = true
}

func (turn *bandsTurn) row(text string) {
	bandsWriteRow(turn.target, bandsTurnRow(turn.ansi, bandsShelliaBackground, colorMagenta, text, boxWidthFor(turn.target)))
	turn.lastRowBlank = text == ""
}

func (turn *bandsTurn) ensurePaddingRow() {
	if turn == nil || turn.closed || turn.lastRowBlank {
		return
	}
	turn.row("")
}

func (turn *bandsTurn) contentWidth(indent string) int {
	width := boxWidthFor(turn.target) - visibleWidth(bandsMarker+"  "+indent) - 2
	if width < 1 {
		return 1
	}
	return width
}

func bandsPlanStepLines(ansi bool, cfg configpkg.Config, plans []core.CommandPlan, width int) []string {
	lines := make([]string, 0, len(plans)*4)
	for index, plan := range plans {
		prefixPlain := fmt.Sprintf("%d. ", index+1)
		prefixRendered := stepBadge(ansi, index+1) + " "
		purposeWidth := width - visibleWidth(prefixPlain)
		if purposeWidth < 1 {
			purposeWidth = 1
		}
		for lineIndex, line := range wrapPlainText(plan.Purpose, purposeWidth) {
			if lineIndex == 0 {
				lines = append(lines, prefixRendered+line)
				continue
			}
			lines = append(lines, strings.Repeat(" ", visibleWidth(prefixPlain))+line)
		}
		lines = append(lines, renderCommandBox(ansi, plan.Command, width)...)
		if cfg.Verbose {
			lines = append(lines,
				fmt.Sprintf("%s %s", metaLabel(ansi, "risk"), riskBadge(ansi, plan.Risk)),
				fmt.Sprintf("%s %s", metaLabel(ansi, "safety"), classificationBadge(ansi, plan.Classification)),
				fmt.Sprintf("%s %s", metaLabel(ansi, "confirm"), confirmBadge(ansi, plan.RequiresConfirmation)),
			)
		}
		lines = append(lines, "")
	}
	return lines
}

func newBandsStepSurface(target io.Writer, ansi bool) *bandsStepSurface {
	if target == nil {
		target = io.Discard
	}
	totalWidth := boxWidthFor(target)
	width := totalWidth - visibleWidth(bandsMarker+"    ") - 2
	if width < 1 {
		width = 1
	}
	return &bandsStepSurface{
		target:     target,
		ansi:       ansi,
		totalWidth: totalWidth,
		width:      width,
	}
}

func (surface *bandsStepSurface) writer() io.Writer {
	if surface == nil || surface.closed {
		return io.Discard
	}
	return surface.target
}

func (surface *bandsStepSurface) ansiEnabled() bool {
	return surface != nil && surface.ansi
}

func (surface *bandsStepSurface) contentWidth() int {
	if surface == nil || surface.closed {
		return 1
	}
	return surface.width
}

func (surface *bandsStepSurface) writeRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}
	for _, row := range wrapRenderedRows(rendered, surface.contentWidth()) {
		bandsWriteRow(surface.target, bandsExecutionRow(surface.ansi, row, surface.totalWidth))
	}
}

func (surface *bandsStepSurface) replaceLastRenderedRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}
	row := bandsExecutionRow(surface.ansi, rendered, surface.totalWidth)
	output, ok := surface.target.(*os.File)
	if !surface.ansi || !ok || !term.IsTerminal(int(output.Fd())) {
		bandsWriteRow(surface.target, row)
		return
	}
	fmt.Fprint(surface.target, "\033[1A\r\033[2K")
	bandsWriteRow(surface.target, row)
}

func (surface *bandsStepSurface) renderEditableRow(rendered string, moveLeft int) {
	if surface == nil || surface.closed {
		return
	}

	row := bandsExecutionRow(surface.ansi, rendered, surface.totalWidth)
	if !surface.ansi {
		fmt.Fprint(surface.target, row)
		return
	}
	padding := bandsRowPadding(bandsMarker+"    "+rendered, surface.totalWidth)
	fmt.Fprint(surface.target, "\r\033[K", row)
	if moveLeft+padding > 0 {
		fmt.Fprintf(surface.target, "\033[%dD", moveLeft+padding)
	}
}

func (surface *bandsStepSurface) close() {
	if surface == nil || surface.closed {
		return
	}
	surface.closed = true
}

func bandsTurnRow(ansi bool, background string, accent string, text string, width int) string {
	if !ansi {
		return bandsMarker + "  " + text
	}
	return bandsANSIRow(background, accent, bandsMarker+"  ", text, width)
}

func bandsExecutionRow(ansi bool, text string, width int) string {
	if !ansi {
		return bandsMarker + "    " + text
	}
	return bandsANSIRow(bandsExecutionBackground, colorDim, bandsMarker+"    ", text, width)
}

func bandsANSIRow(background string, accent string, prefix string, text string, width int) string {
	text = strings.NewReplacer(
		colorReset, colorReset+background,
		"\033[m", "\033[m"+background,
	).Replace(text)
	row := background + accent + prefix + colorReset + background + text
	if padding := bandsRowPadding(prefix+text, width); padding > 0 {
		row += strings.Repeat(" ", padding)
	}
	return row + colorReset
}

func bandsRowPadding(content string, width int) int {
	padding := width - visibleWidth(content)
	if padding < 0 {
		return 0
	}
	return padding
}

func bandsWriteRow(target io.Writer, row string) {
	fmt.Fprint(target, row, "\r\n")
}

func bandsWriteBlank(target io.Writer) {
	fmt.Fprint(target, "\r\n")
}

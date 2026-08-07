package ui

import (
	"fmt"
	"io"
	"strings"

	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

const (
	bandsMarker              = "▌"
	bandsUserBackground      = "\033[48;5;24m"
	bandsShelliaBackground   = "\033[48;5;53m"
	bandsExecutionBackground = "\033[48;5;236m"
)

type bandsRenderer struct {
	target io.Writer
	ansi   bool
}

type bandsTurn struct {
	target io.Writer
	ansi   bool
	closed bool
}

type bandsStepSurface struct {
	rows       *rowSurface
	ansi       bool
	totalWidth int
}

func newBandsRenderer(target io.Writer, ansi bool) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &bandsRenderer{target: target, ansi: ansi}
}

func (renderer *bandsRenderer) userTurn(mode core.InteractiveMode, text string) {
	if renderer == nil {
		return
	}

	width := boxWidthFor(renderer.target)
	prompt := promptPrefix(renderer.ansi, mode)
	prefix := "  " + prompt
	contentWidth := width - visibleWidth(bandsMarker+" "+prefix)
	if contentWidth < 1 {
		contentWidth = 1
	}

	fmt.Fprintln(renderer.target)
	fmt.Fprintln(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, style(renderer.ansi, colorCyan+colorBold, "Tu"), width))
	for index, line := range wrapPromptRunes([]rune(text), contentWidth) {
		if index == 0 {
			line = prefix + style(renderer.ansi, colorWhite, line)
		} else {
			line = "  " + strings.Repeat(" ", visibleWidth(prompt)) + style(renderer.ansi, colorWhite, line)
		}
		fmt.Fprintln(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, line, width))
	}
}

func (renderer *bandsRenderer) beginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) turnImpl {
	if renderer == nil {
		return &bandsTurn{target: io.Discard}
	}

	turn := &bandsTurn{target: renderer.target, ansi: renderer.ansi}
	fmt.Fprintln(turn.target)
	turn.row(style(turn.ansi, colorMagenta+colorBold, "Shellia"))
	if context := plainHeaderContextValue(cfg, ctxInfo); context != "" {
		turn.row("  " + style(turn.ansi, colorDim, context))
	}
	return turn
}

func (turn *bandsTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if turn == nil || turn.closed {
		return
	}

	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	turn.row("  " + style(turn.ansi, titleColor+colorBold, title))
	for _, line := range wrapPlainText(summary, surfaceContentWidth(turn.target, bandsMarker+"     ")) {
		turn.row("    " + style(turn.ansi, colorWhite+colorBold, line))
	}

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		return
	}

	turn.row("  " + style(turn.ansi, colorDim+colorBold, "steps"))
	for _, line := range planStepLines(turn.target, turn.ansi, cfg, plans) {
		turn.row("    " + line)
	}
}

func (turn *bandsTurn) beginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *stepBox {
	if turn == nil || turn.closed {
		return nil
	}

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

	turn.row("  " + style(turn.ansi, colorMagenta+colorBold, "Shellia"))
	for _, line := range renderAnswerMarkdown(message, surfaceContentWidth(turn.target, bandsMarker+"     "), turn.ansi) {
		turn.row("    " + line)
	}
}

func (turn *bandsTurn) suspend() {}

func (turn *bandsTurn) resume() {}

func (turn *bandsTurn) close() {
	if turn == nil || turn.closed {
		return
	}
	fmt.Fprintln(turn.target)
	turn.closed = true
}

func (turn *bandsTurn) row(text string) {
	fmt.Fprintln(turn.target, bandsTurnRow(turn.ansi, bandsShelliaBackground, colorMagenta, text, boxWidthFor(turn.target)))
}

func newBandsStepSurface(target io.Writer, ansi bool) *bandsStepSurface {
	if target == nil {
		target = io.Discard
	}
	totalWidth := boxWidthFor(target)
	return &bandsStepSurface{
		rows: newRowSurface(rowSurfaceSpec{
			target: target,
			ansi:   ansi,
			width:  surfaceContentWidth(target, bandsMarker+"     "),
		}),
		ansi:       ansi,
		totalWidth: totalWidth,
	}
}

func (surface *bandsStepSurface) writer() io.Writer {
	if surface == nil || surface.rows == nil {
		return io.Discard
	}
	return surface.rows.writer()
}

func (surface *bandsStepSurface) ansiEnabled() bool {
	return surface != nil && surface.ansi
}

func (surface *bandsStepSurface) contentWidth() int {
	if surface == nil || surface.rows == nil {
		return 1
	}
	return surface.rows.contentWidth()
}

func (surface *bandsStepSurface) writeRow(rendered string) {
	if surface == nil || surface.rows == nil {
		return
	}
	surface.rows.writeRow(bandsExecutionRow(surface.ansi, rendered, surface.totalWidth))
}

func (surface *bandsStepSurface) replaceLastRenderedRow(rendered string) {
	if surface == nil || surface.rows == nil {
		return
	}
	row := bandsExecutionRow(surface.ansi, rendered, surface.totalWidth)
	if !surface.ansi {
		surface.rows.writeRow(row)
		return
	}
	surface.rows.replaceLastRenderedRow(row)
}

func (surface *bandsStepSurface) renderEditableRow(rendered string, moveLeft int) {
	if surface == nil || surface.rows == nil {
		return
	}
	if !surface.ansi {
		surface.rows.writeRow(bandsExecutionRow(false, rendered, surface.totalWidth))
		return
	}

	row := bandsExecutionRow(true, rendered, surface.totalWidth)
	padding := bandsRowPadding(bandsMarker+"     "+rendered, surface.totalWidth)
	surface.rows.renderEditableRow(row, moveLeft+padding)
}

func (surface *bandsStepSurface) close() {
	if surface == nil || surface.rows == nil {
		return
	}
	surface.rows.close()
}

func bandsTurnRow(ansi bool, background string, accent string, text string, width int) string {
	if !ansi {
		return bandsMarker + " " + text
	}
	return bandsANSIRow(background, accent, bandsMarker+" ", text, width)
}

func bandsExecutionRow(ansi bool, text string, width int) string {
	if !ansi {
		return bandsMarker + "     " + text
	}
	return bandsANSIRow(bandsExecutionBackground, colorDim, bandsMarker+"     ", text, width)
}

func bandsANSIRow(background string, accent string, prefix string, text string, width int) string {
	text = strings.ReplaceAll(text, colorReset, colorReset+background)
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

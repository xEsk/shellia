package ui

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

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
	target io.Writer
	ansi   bool
	width  int
	closed bool
}

func newBandsRenderer(target io.Writer, ansi bool) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &bandsRenderer{target: target, ansi: ansi}
}

func (renderer *bandsRenderer) userTurn(_ core.InteractiveMode, text string) {
	if renderer == nil {
		return
	}

	fmt.Fprintln(renderer.target)
	fmt.Fprintln(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, style(renderer.ansi, colorCyan+colorBold, "Tu")))
	for index, line := range wrapPlainText(text, surfaceContentWidth(renderer.target, bandsMarker+"     ")) {
		if index == 0 {
			line = style(renderer.ansi, colorCyan+colorBold, "you") + style(renderer.ansi, colorWhite, " › ") + style(renderer.ansi, colorWhite, line)
		} else {
			line = "      " + style(renderer.ansi, colorWhite, line)
		}
		fmt.Fprintln(renderer.target, bandsTurnRow(renderer.ansi, bandsUserBackground, colorCyan, "  "+line))
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

	for _, line := range renderAnswerMarkdown(message, surfaceContentWidth(turn.target, bandsMarker+"     "), turn.ansi) {
		turn.row("  " + line)
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
	fmt.Fprintln(turn.target, bandsTurnRow(turn.ansi, bandsShelliaBackground, colorMagenta, text))
}

func newBandsStepSurface(target io.Writer, ansi bool) *bandsStepSurface {
	if target == nil {
		target = io.Discard
	}
	return &bandsStepSurface{
		target: target,
		ansi:   ansi,
		width:  surfaceContentWidth(target, bandsMarker+"     "),
	}
}

func (surface *bandsStepSurface) writer() io.Writer {
	return surface.target
}

func (surface *bandsStepSurface) ansiEnabled() bool {
	return surface.ansi
}

func (surface *bandsStepSurface) contentWidth() int {
	return surface.width
}

func (surface *bandsStepSurface) writeRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}
	fmt.Fprintln(surface.target, bandsExecutionRow(surface.ansi, rendered))
}

func (surface *bandsStepSurface) replaceLastRenderedRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}

	output, ok := surface.target.(*os.File)
	if !surface.ansi || !ok || !term.IsTerminal(int(output.Fd())) {
		surface.writeRow(rendered)
		return
	}
	fmt.Fprint(surface.target, "\033[1A\r\033[2K")
	surface.writeRow(rendered)
}

func (surface *bandsStepSurface) renderEditableRow(rendered string, moveLeft int) {
	if surface == nil || surface.closed {
		return
	}
	if !surface.ansi {
		surface.writeRow(rendered)
		return
	}

	fmt.Fprint(surface.target, "\r\033[K", bandsExecutionRowStart(surface.ansi), rendered)
	if moveLeft > 0 {
		fmt.Fprintf(surface.target, "\033[%dD", moveLeft)
	}
}

func (surface *bandsStepSurface) close() {
	if surface == nil || surface.closed {
		return
	}
	surface.closed = true
}

func bandsTurnRow(ansi bool, background string, accent string, text string) string {
	if !ansi {
		return bandsMarker + " " + text
	}
	return background + accent + bandsMarker + colorReset + background + " " + text + colorReset
}

func bandsExecutionRow(ansi bool, text string) string {
	return bandsExecutionRowStart(ansi) + text + bandsExecutionRowEnd(ansi)
}

func bandsExecutionRowStart(ansi bool) string {
	if !ansi {
		return bandsMarker + "     "
	}
	return bandsExecutionBackground + colorDim + bandsMarker + colorReset + bandsExecutionBackground + "     "
}

func bandsExecutionRowEnd(ansi bool) string {
	if !ansi {
		return ""
	}
	return colorReset
}

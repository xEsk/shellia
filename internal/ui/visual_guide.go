package ui

import (
	"fmt"
	"io"
	"strings"

	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

type guideRenderer struct {
	target io.Writer
	ansi   bool
}

type guideTurn struct {
	target io.Writer
	ansi   bool
	closed bool
}

type guideStepSurface struct {
	surface *rowSurface
	ansi    bool
}

func newGuideRenderer(target io.Writer, ansi bool) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &guideRenderer{target: target, ansi: ansi}
}

func (renderer *guideRenderer) userTurn(mode core.InteractiveMode, text string) {
	prefix := guideRail(renderer.ansi, colorCyan)
	guideWrite(renderer.target, prefix, style(renderer.ansi, colorCyan+colorBold, "Tu"))

	prompt := promptPrefix(renderer.ansi, mode)
	promptWidth := visibleWidth(prompt)
	for index, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if index == 0 {
			guideWrite(renderer.target, prefix, prompt+style(renderer.ansi, colorWhite, line))
			continue
		}
		guideWrite(renderer.target, prefix, strings.Repeat(" ", promptWidth)+style(renderer.ansi, colorWhite, line))
	}
}

func (renderer *guideRenderer) beginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) turnImpl {
	turn := &guideTurn{target: renderer.target, ansi: renderer.ansi}
	turn.write("")
	turn.write(style(turn.ansi, colorMagenta+colorBold, "Shellia"))
	turn.write("  " + style(turn.ansi, colorMagenta+colorBold, "Shellia") + style(turn.ansi, colorDim, " · "+fallbackValue(strings.TrimSpace(version), "dev")))
	if context := plainHeaderContextValue(cfg, ctxInfo); context != "" {
		turn.write("  " + style(turn.ansi, colorDim, context))
	}
	return turn
}

func (turn *guideTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if turn == nil || turn.closed {
		return
	}

	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	turn.write("  " + style(turn.ansi, titleColor+colorBold, title))
	for _, line := range wrapPlainText(summary, turn.contentWidth("    ")) {
		turn.write("    " + style(turn.ansi, colorWhite+colorBold, line))
	}

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		return
	}

	turn.write("  " + style(turn.ansi, colorDim+colorBold, "steps"))
	for _, line := range planStepLines(turn.target, turn.ansi, cfg, plans) {
		turn.write("    " + line)
	}
}

func (turn *guideTurn) beginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *stepBox {
	if turn == nil || turn.closed {
		return nil
	}

	turn.write("  " + style(turn.ansi, colorMagenta+colorBold, fmt.Sprintf("step %d/%d", index, total)))
	surface := newRowSurface(rowSurfaceSpec{
		target: turn.target,
		ansi:   turn.ansi,
		width:  surfaceContentWidth(turn.target, guideRail(turn.ansi, colorDim)+"     "),
		prefix: guideRail(turn.ansi, colorDim) + "     ",
	})
	box := newStepBoxForSurface(&guideStepSurface{surface: surface, ansi: turn.ansi})
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

func (turn *guideTurn) final(message string) {
	if turn == nil || turn.closed {
		return
	}

	turn.write("  " + style(turn.ansi, colorMagenta+colorBold, "Shellia"))
	for _, line := range renderAnswerMarkdown(message, turn.contentWidth("    "), turn.ansi) {
		turn.write("    " + line)
	}
}

func (turn *guideTurn) suspend() {}

func (turn *guideTurn) resume() {}

func (turn *guideTurn) close() {
	if turn == nil || turn.closed {
		return
	}
	fmt.Fprintln(turn.target, style(turn.ansi, colorDim, strings.Repeat("─", boxWidthFor(turn.target))))
	turn.closed = true
}

func (turn *guideTurn) write(content string) {
	if turn == nil || turn.closed {
		return
	}
	guideWrite(turn.target, guideRail(turn.ansi, colorMagenta), content)
}

func (turn *guideTurn) contentWidth(indent string) int {
	width := boxWidthFor(turn.target) - visibleWidth("│"+indent)
	if width < 1 {
		return 1
	}
	return width
}

func (surface *guideStepSurface) writer() io.Writer {
	return surface.surface.writer()
}

func (surface *guideStepSurface) ansiEnabled() bool {
	return surface.surface.ansiEnabled()
}

func (surface *guideStepSurface) contentWidth() int {
	return surface.surface.contentWidth()
}

func (surface *guideStepSurface) writeRow(rendered string) {
	if stripANSISequences(rendered) == "• system output" {
		rendered = style(surface.ansi, colorDim, "system output")
	}
	surface.surface.writeRow(rendered)
}

func (surface *guideStepSurface) replaceLastRenderedRow(rendered string) {
	surface.surface.replaceLastRenderedRow(rendered)
}

func (surface *guideStepSurface) renderEditableRow(rendered string, moveLeft int) {
	surface.surface.renderEditableRow(rendered, moveLeft)
}

func (surface *guideStepSurface) close() {
	surface.surface.close()
}

func guideRail(ansi bool, color string) string {
	return style(ansi, color, "│")
}

func guideWrite(target io.Writer, rail string, content string) {
	fmt.Fprintln(target, rail+" "+content)
}

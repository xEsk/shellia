package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/xEsk/shellia/internal/core"

	configpkg "github.com/xEsk/shellia/internal/config"
)

const (
	guideUserBackground = "\033[48;2;17;49;58m"
	guideStepBackground = "\033[48;2;52;66;71m"
)

type guideRenderer struct {
	target io.Writer
	ansi   bool
	user   string
}

type guideTurn struct {
	target       io.Writer
	ansi         bool
	lastRowBlank bool
	closed       bool
}

type guideStepSurface struct {
	surface *rowSurface
}

func newGuideRenderer(target io.Writer, ansi bool) rendererImpl {
	return newGuideRendererWithUser(target, ansi, "")
}

func newGuideRendererWithUser(target io.Writer, ansi bool, user string) rendererImpl {
	if target == nil {
		target = io.Discard
	}
	return &guideRenderer{target: target, ansi: ansi, user: strings.TrimSpace(user)}
}

func (renderer *guideRenderer) ownsUserTurnQuestion() bool {
	return true
}

func (renderer *guideRenderer) interactivePromptPrefix(mode core.InteractiveMode) string {
	if mode != core.InteractiveModeAI {
		return promptPrefix(renderer.ansi, mode)
	}
	user := fallbackValue(renderer.user, "you")
	return style(renderer.ansi, colorCyan+colorBold, user) + style(renderer.ansi, colorWhite, " › ")
}

func (renderer *guideRenderer) userTurn(mode core.InteractiveMode, text string) {
	prefix := guideRail(renderer.ansi, colorCyan)
	contentWidth := surfaceContentWidth(renderer.target, prefix+"    ")
	messageWidth := contentWidth - 2
	if messageWidth < 1 {
		messageWidth = 1
	}
	rows := make([]string, 0, 3)
	for _, line := range wrapPromptRunes([]rune(fallbackValue(renderer.user, "you")), contentWidth) {
		rows = append(rows, style(renderer.ansi, colorCyan+colorBold, line))
	}
	for _, line := range wrapPromptRunes([]rune(text), messageWidth) {
		rows = append(rows, "  "+style(renderer.ansi, colorWhite, line))
	}

	surfaceWidth := 1
	for _, row := range rows {
		if width := visibleWidth(row); width > surfaceWidth {
			surfaceWidth = width
		}
	}
	guideUserSurfaceWrite(renderer.target, renderer.ansi, prefix, "", surfaceWidth)
	for _, row := range rows {
		guideUserSurfaceWrite(renderer.target, renderer.ansi, prefix, row, surfaceWidth)
	}
	guideUserSurfaceWrite(renderer.target, renderer.ansi, prefix, "", surfaceWidth)
}

func (renderer *guideRenderer) beginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) turnImpl {
	turn := &guideTurn{target: renderer.target, ansi: renderer.ansi}
	fmt.Fprintln(turn.target)
	turn.write(shelliaBrand(turn.ansi, false) + style(turn.ansi, colorDim, " · "+fallbackValue(strings.TrimSpace(version), "dev")))
	if context := plainHeaderContextValue(cfg, ctxInfo); context != "" {
		turn.write(style(turn.ansi, colorDim, context))
	}
	turn.write("")
	return turn
}

func (turn *guideTurn) plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
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
	turn.write(style(turn.ansi, titleColor+colorBold, title))
	for _, line := range wrapPlainText(summary, turn.contentWidth("  ")) {
		turn.write("  " + style(turn.ansi, colorWhite, line))
	}

	if len(plans) == 0 || (!cfg.Verbose && !cfg.PlanOnly && !cfg.AskConfirmPlan) {
		return
	}

	turn.write("")
	turn.write(style(turn.ansi, colorDim+colorBold, "steps"))
	for _, line := range guidePlanStepLines(turn.ansi, cfg, plans, turn.contentWidth("")) {
		turn.write(line)
	}
}

func (turn *guideTurn) beginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *stepBox {
	if turn == nil || turn.closed {
		return nil
	}

	turn.write("")
	nestedPrefix := guideRail(turn.ansi, colorMagenta) + "   " + guideTechnicalRail(turn.ansi, colorDim)
	surface := newRowSurface(rowSurfaceSpec{
		target: turn.target,
		ansi:   turn.ansi,
		width:  surfaceContentWidth(turn.target, nestedPrefix),
		prefix: nestedPrefix,
	})
	guideSurface := &guideStepSurface{surface: surface}
	guideSurface.writeRow("")
	guideSurface.writeRow(style(turn.ansi, commandBoxPromptForeground+colorBold, fmt.Sprintf("step %d/%d", index, total)))
	turn.lastRowBlank = false
	box := newStepBoxForSurface(guideSurface)
	box.Spacer()
	box.Command(plan.Command)
	box.Spacer()
	box.writePrefixed("• ", style(turn.ansi, colorDim, "• "), plan.Purpose, colorDim)
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

	turn.ensurePaddingRow()
	turn.write(shelliaBrand(turn.ansi, false))
	for _, line := range renderAnswerMarkdown(message, turn.contentWidth("  "), turn.ansi) {
		turn.write("  " + line)
	}
}

func (turn *guideTurn) thinkingPrefix() string {
	if turn == nil || turn.closed {
		return ""
	}
	turn.ensurePaddingRow()
	return guideRail(turn.ansi, colorMagenta) + "   "
}

func (turn *guideTurn) suspend() {}

func (turn *guideTurn) resume() {}

func (turn *guideTurn) close() {
	if turn == nil || turn.closed {
		return
	}
	turn.closed = true
}

func (turn *guideTurn) write(content string) {
	if turn == nil || turn.closed {
		return
	}
	guideWrite(turn.target, guideRail(turn.ansi, colorMagenta), content)
	turn.lastRowBlank = content == ""
}

func (turn *guideTurn) ensurePaddingRow() {
	if turn == nil || turn.closed || turn.lastRowBlank {
		return
	}
	turn.write("")
}

func (turn *guideTurn) contentWidth(indent string) int {
	width := boxWidthFor(turn.target) - visibleWidth("┃ "+indent)
	if width < 1 {
		return 1
	}
	return width
}

func guidePlanStepLines(ansi bool, cfg configpkg.Config, plans []core.CommandPlan, width int) []string {
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

func (surface *guideStepSurface) writer() io.Writer {
	return surface.surface.writer()
}

func (surface *guideStepSurface) ansiEnabled() bool {
	return surface.surface.ansiEnabled()
}

func (surface *guideStepSurface) contentWidth() int {
	width := surface.surface.contentWidth() - 1
	if width < 1 {
		return 1
	}
	return width
}

func (surface *guideStepSurface) writeRow(rendered string) {
	for _, row := range wrapRenderedRows(rendered, surface.contentWidth()) {
		surface.surface.writeRow(guideStepRow(surface.surface.ansiEnabled(), row, surface.surface.contentWidth()))
	}
}

func (surface *guideStepSurface) replaceLastRenderedRow(rendered string) {
	surface.surface.replaceLastRenderedRow(guideStepRow(surface.surface.ansiEnabled(), rendered, surface.surface.contentWidth()))
}

func (surface *guideStepSurface) renderEditableRow(rendered string, moveLeft int) {
	padding := surface.contentWidth() - visibleWidth(rendered)
	if padding < 0 {
		padding = 0
	}
	surface.surface.renderEditableRow(
		guideStepRow(surface.surface.ansiEnabled(), rendered, surface.surface.contentWidth()),
		moveLeft+padding,
	)
}

func (surface *guideStepSurface) close() {
	surface.writeRow("")
	surface.surface.close()
}

func guideStepRow(ansi bool, rendered string, width int) string {
	rendered = " " + rendered
	if !ansi {
		return rendered
	}
	rendered = strings.NewReplacer(
		colorReset, colorReset+guideStepBackground,
		"\033[m", "\033[m"+guideStepBackground,
	).Replace(rendered)
	if padding := width - visibleWidth(rendered); padding > 0 {
		rendered += strings.Repeat(" ", padding)
	}
	return guideStepBackground + rendered + colorReset
}

func guideRail(ansi bool, color string) string {
	return style(ansi, color, "┃")
}

func guideTechnicalRail(ansi bool, color string) string {
	return style(ansi, color, "│")
}

func guideWrite(target io.Writer, rail string, content string) {
	fmt.Fprintln(target, rail+" "+content)
}

func guideUserSurfaceWrite(target io.Writer, ansi bool, rail string, content string, width int) {
	rightPadding := width - visibleWidth(content) + 2
	if rightPadding < 2 {
		rightPadding = 2
	}
	row := " " + content + strings.Repeat(" ", rightPadding)
	if ansi {
		row = strings.NewReplacer(
			colorReset, colorReset+guideUserBackground,
			"\033[m", "\033[m"+guideUserBackground,
		).Replace(row)
		row = guideUserBackground + row + colorReset
	}
	fmt.Fprint(target, rail+row+"\r\n")
}

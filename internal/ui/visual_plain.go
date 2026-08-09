package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/xEsk/shellia/internal/core"
)

type plainRenderer struct {
	target io.Writer
	ansi   bool
	user   string
}

type plainTurn struct {
	target io.Writer
	ansi   bool
	closed bool
}

func newPlainRenderer(target io.Writer, ansi bool) rendererImpl {
	return newPlainRendererWithUser(target, ansi, "")
}

func newPlainRendererWithUser(target io.Writer, ansi bool, user string) rendererImpl {
	return &plainRenderer{target: target, ansi: ansi, user: strings.TrimSpace(user)}
}

func (renderer *plainRenderer) interactivePromptPrefix(mode core.InteractiveMode) string {
	if mode != core.InteractiveModeAI {
		return promptPrefix(renderer.ansi, mode)
	}
	user := fallbackValue(renderer.user, "you")
	return style(renderer.ansi, colorCyan+colorBold, user) + style(renderer.ansi, colorWhite, " › ")
}

func (renderer *plainRenderer) userTurn(mode core.InteractiveMode, text string) {
	prompt := renderer.interactivePromptPrefix(mode)
	printSubmittedPromptTo(renderer.target, renderer.ansi, prompt, []rune(text))
	fmt.Fprint(renderer.target, "\r\n")
}

func (renderer *plainRenderer) beginShelliaTurn(options ViewOptions, ctxInfo core.ContextInfo) turnImpl {
	printHeaderTo(renderer.target, renderer.ansi, options, ctxInfo)
	return &plainTurn{target: renderer.target, ansi: renderer.ansi}
}

func (turn *plainTurn) plan(options ViewOptions, summary string, plans []core.CommandPlan, discovery bool) {
	if turn == nil || turn.closed {
		return
	}
	printPlanTo(turn.target, turn.ansi, options, summary, plans, discovery)
}

func (turn *plainTurn) beginStep(options ViewOptions, index int, total int, plan core.CommandPlan) *stepBox {
	if turn == nil || turn.closed {
		return nil
	}
	return printCommandExecutionTo(turn.target, turn.ansi, options, index, total, plan)
}

func (turn *plainTurn) final(message string) {
	if turn == nil || turn.closed {
		return
	}
	printFinalResultTo(turn.target, turn.ansi, message)
}

func (turn *plainTurn) suspend() {}

func (turn *plainTurn) resume() {}

func (turn *plainTurn) close() {
	if turn == nil {
		return
	}
	turn.closed = true
}

func newPlainStepBox(target io.Writer, ansi bool, title string) *stepBox {
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ansi, colorDim, strings.Repeat("─", boxWidthFor(target))))
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ansi, colorMagenta+colorBold, title))
	surface := newRowSurface(rowSurfaceSpec{
		target: target,
		ansi:   ansi,
		width:  surfaceContentWidth(target, ""),
	})
	return newStepBoxForSurface(surface)
}

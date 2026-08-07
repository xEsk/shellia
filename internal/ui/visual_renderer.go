package ui

import (
	"io"

	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

// Presentation describes the effective visual style and ANSI capability.
type Presentation struct {
	Style configpkg.VisualStyle
	ANSI  bool
	User  string
}

// Renderer presents user and Shellia turns through one selected visual style.
type Renderer struct {
	impl rendererImpl
	ansi bool
}

// Turn presents the semantic parts of one Shellia response.
type Turn struct {
	impl turnImpl
}

type rendererImpl interface {
	userTurn(core.InteractiveMode, string)
	beginShelliaTurn(configpkg.Config, core.ContextInfo) turnImpl
}

type userTurnQuestionOwner interface {
	ownsUserTurnQuestion() bool
}

type interactivePromptPrefixProvider interface {
	interactivePromptPrefix(core.InteractiveMode) string
}

type thinkingPrefixProvider interface {
	thinkingPrefix() string
}

type turnImpl interface {
	plan(configpkg.Config, string, []core.CommandPlan, bool)
	beginStep(configpkg.Config, int, int, core.CommandPlan) *stepBox
	final(string)
	suspend()
	resume()
	close()
}

// NewRenderer selects the visual implementation for one output stream.
func NewRenderer(target io.Writer, presentation Presentation) *Renderer {
	if target == nil {
		target = io.Discard
	}

	var impl rendererImpl
	switch presentation.Style {
	case configpkg.VisualStylePlain:
		impl = newPlainRenderer(target, presentation.ANSI)
	case configpkg.VisualStyleGuide:
		impl = newGuideRendererWithUser(target, presentation.ANSI, presentation.User)
	case configpkg.VisualStyleBands:
		impl = newBandsRenderer(target, presentation.ANSI)
	case configpkg.VisualStyleCards:
		impl = newCardsRenderer(target, presentation.ANSI)
	default:
		impl = newPlainRenderer(target, presentation.ANSI)
	}

	return &Renderer{impl: impl, ansi: presentation.ANSI}
}

// UserTurn presents one submitted interactive prompt.
func (renderer *Renderer) UserTurn(mode core.InteractiveMode, text string) {
	if renderer == nil || renderer.impl == nil {
		return
	}
	renderer.impl.userTurn(mode, text)
}

func (renderer *Renderer) ownsUserTurnQuestion(mode core.InteractiveMode) bool {
	if renderer == nil || renderer.impl == nil || mode != core.InteractiveModeAI {
		return false
	}
	owner, ok := renderer.impl.(userTurnQuestionOwner)
	return ok && owner.ownsUserTurnQuestion()
}

func (renderer *Renderer) interactivePromptPrefix(ui bool, mode core.InteractiveMode) string {
	if renderer != nil && renderer.impl != nil {
		if provider, ok := renderer.impl.(interactivePromptPrefixProvider); ok {
			return provider.interactivePromptPrefix(mode)
		}
	}
	return promptPrefix(ui, mode)
}

// BeginShelliaTurn opens one Shellia response turn.
func (renderer *Renderer) BeginShelliaTurn(cfg configpkg.Config, ctxInfo core.ContextInfo) *Turn {
	if renderer == nil || renderer.impl == nil {
		return &Turn{}
	}
	return &Turn{impl: renderer.impl.beginShelliaTurn(cfg, ctxInfo)}
}

// Plan presents one planning decision.
func (turn *Turn) Plan(cfg configpkg.Config, summary string, plans []core.CommandPlan, discovery bool) {
	if turn == nil || turn.impl == nil {
		return
	}
	turn.impl.plan(cfg, summary, plans, discovery)
}

// BeginStep opens one command execution surface.
func (turn *Turn) BeginStep(cfg configpkg.Config, index int, total int, plan core.CommandPlan) *StepBox {
	if turn == nil || turn.impl == nil {
		return nil
	}
	return turn.impl.beginStep(cfg, index, total, plan)
}

// Final presents the terminal answer for this turn.
func (turn *Turn) Final(message string) {
	if turn == nil || turn.impl == nil {
		return
	}
	turn.impl.final(message)
}

// ThinkingPrefix returns the visual prefix for an in-progress model request.
func (turn *Turn) ThinkingPrefix() string {
	if turn == nil || turn.impl == nil {
		return ""
	}
	provider, ok := turn.impl.(thinkingPrefixProvider)
	if !ok {
		return ""
	}
	return provider.thinkingPrefix()
}

// Suspend temporarily leaves the visual surface before terminal handoff.
func (turn *Turn) Suspend() {
	if turn == nil || turn.impl == nil {
		return
	}
	turn.impl.suspend()
}

// Resume restores the visual surface after terminal handoff.
func (turn *Turn) Resume() {
	if turn == nil || turn.impl == nil {
		return
	}
	turn.impl.resume()
}

// Close completes the visual surface for this turn.
func (turn *Turn) Close() {
	if turn == nil || turn.impl == nil {
		return
	}
	turn.impl.close()
}

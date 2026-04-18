package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// thinkingIndicator renders a subtle animated Thinking status while the LLM is busy.
type thinkingIndicator struct {
	stopCh chan struct{}
	doneCh chan struct{}
}

const thinkingStatusText = "Thinking..."

// startThinkingIndicator renders a subtle animated Thinking status on the current line.
func startThinkingIndicator(ui bool, target io.Writer) *thinkingIndicator {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}

	indicator := &thinkingIndicator{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	go func() {
		defer close(indicator.doneCh)

		const (
			initialDelay = 180 * time.Millisecond
			frameDelay   = 50 * time.Millisecond
		)

		delayTimer := time.NewTimer(initialDelay)
		defer delayTimer.Stop()

		ticker := time.NewTicker(frameDelay)
		defer ticker.Stop()

		frame := 0
		shown := false

		for {
			select {
			case <-indicator.stopCh:
				if shown {
					clearThinkingLine(target)
				}
				return
			case <-delayTimer.C:
				renderThinkingFrame(target, ui, frame, true)
				shown = true
				frame++
			case <-ticker.C:
				if !shown {
					continue
				}
				renderThinkingFrame(target, ui, frame, false)
				frame++
			}
		}
	}()

	return indicator
}

// stop terminates the Thinking animation and clears it from the terminal.
func (indicator *thinkingIndicator) stop() {
	if indicator == nil {
		return
	}
	close(indicator.stopCh)
	<-indicator.doneCh
}

// clearThinkingLine removes the current Thinking status line from the terminal.
func clearThinkingLine(target io.Writer) {
	fmt.Fprint(target, "\r\033[2K")
}

// renderThinkingFrame repaints the current line with the requested Thinking frame.
func renderThinkingFrame(target io.Writer, ui bool, frame int, firstFrame bool) {
	if firstFrame {
		fmt.Fprint(target, "\n")
	}
	fmt.Fprint(target, "\r\033[2K")
	fmt.Fprint(target, thinkingStatusFrame(ui, frame))
}

// thinkingStatusFrame returns one frame of the animated Thinking status.
func thinkingStatusFrame(ui bool, frame int) string {
	if !ui {
		return thinkingStatusText
	}

	runes := []rune(thinkingStatusText)
	center := thinkingShimmerCenter(frame, len(runes))

	var b strings.Builder
	for index, r := range runes {
		b.WriteString(thinkingRuneStyle(thinkingShimmerWeight(float64(index), center)))
		b.WriteRune(r)
	}
	b.WriteString(colorReset)

	return b.String()
}

// thinkingShimmerCenter computes the moving highlight center for the shimmer effect.
func thinkingShimmerCenter(frame int, textLen int) float64 {
	if textLen <= 0 {
		return 0
	}
	steps := (textLen + 6) * 2
	return float64(frame%steps)/2 - 2
}

// thinkingShimmerWeight returns the highlight intensity for a rune at the given position.
func thinkingShimmerWeight(index float64, center float64) float64 {
	const radius = 3.6

	distance := math.Abs(index - center)
	if distance >= radius {
		return 0
	}

	weight := 1 - (distance / radius)
	return weight * weight * (3 - 2*weight)
}

// thinkingRuneStyle returns the ANSI style for one rune of the shimmer animation.
func thinkingRuneStyle(weight float64) string {
	baseR, baseG, baseB := 118, 124, 136
	peakR, peakG, peakB := 214, 234, 248

	r := blendColorChannel(baseR, peakR, weight)
	g := blendColorChannel(baseG, peakG, weight)
	b := blendColorChannel(baseB, peakB, weight)

	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// blendColorChannel interpolates one RGB channel between the base and peak values.
func blendColorChannel(base int, peak int, weight float64) int {
	if weight <= 0 {
		return base
	}
	if weight >= 1 {
		return peak
	}
	return int(math.Round(float64(base) + (float64(peak-base) * weight)))
}

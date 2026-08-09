package ui

import (
	"fmt"
	"io"
	"os"
)

const (
	sessionBannerMinWidth     = 58
	sessionBannerShortcutLine = "!<cmd>  /shell  /ai  /model  /mode  exit  /clear  /context"
)

// printSessionBanner shows the polished startup banner for the interactive session.
func printSessionBanner(ui bool, options ViewOptions) {
	printSessionBannerTo(os.Stdout, ui, options)
}

// printSessionBannerTo shows the polished startup banner on the provided target.
func printSessionBannerTo(target io.Writer, ui bool, options ViewOptions) {
	fmt.Fprintln(target)
	renderSessionBanner(target, ui, options, boxWidthFor(target))
}

// renderSessionBanner writes the startup banner using a compact grey panel when ANSI is available.
func renderSessionBanner(target io.Writer, ui bool, options ViewOptions, maxWidth int) {
	for _, line := range sessionBannerLines(ui, options, maxWidth) {
		fmt.Fprintln(target, line)
	}
}

// sessionBannerLines returns the visible startup banner rows.
func sessionBannerLines(ui bool, options ViewOptions, maxWidth int) []string {
	if !ui {
		return []string{
			sessionBannerTitlePlain(),
			"  model · " + plainHeaderModelValue(options),
			"  " + sessionBannerShortcutLine,
		}
	}

	width := sessionBannerWidth(options, maxWidth)
	return []string{
		greyPanelBlankRow(width),
		greyPanelRow(width, sessionBannerTitleSegments()...),
		greyPanelRow(width, greyPanelSegment{text: "model · " + plainHeaderModelValue(options), color: greyPanelForeground + colorDim}),
		greyPanelRow(width, greyPanelSegment{text: sessionBannerShortcutLine, color: greyPanelForeground + colorDim}),
		greyPanelBlankRow(width),
	}
}

// sessionBannerTitlePlain returns the no-colour title line.
func sessionBannerTitlePlain() string {
	return shelliaBrand(false, true) + " session · " + shelliaVersionBadge(false)
}

// sessionBannerTitleSegments returns the styled title segments for the ANSI banner.
func sessionBannerTitleSegments() []greyPanelSegment {
	return []greyPanelSegment{
		{text: "shell", color: colorWhite + colorBold},
		{text: "ia", color: colorCyan + colorBold},
		{text: " session · ", color: greyPanelForeground + colorDim},
		{text: shelliaVersionBadge(false), color: colorCyan + colorBold},
	}
}

// sessionBannerWidth returns the compact panel width required for the banner.
func sessionBannerWidth(options ViewOptions, maxWidth int) int {
	width := max(
		visibleWidth(sessionBannerTitlePlain()),
		visibleWidth("model · "+plainHeaderModelValue(options)),
		visibleWidth(sessionBannerShortcutLine),
	) + (greyPanelHorizontalPadding * 2)

	if width < sessionBannerMinWidth {
		width = sessionBannerMinWidth
	}
	if maxWidth > 0 && width > maxWidth {
		width = maxWidth
	}
	return width
}

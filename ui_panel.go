package main

import "strings"

const (
	greyPanelHorizontalPadding = 2
	greyPanelBackground        = "\033[48;5;238m"
	greyPanelForeground        = "\033[38;5;255m"
)

type greyPanelSegment struct {
	text  string
	color string
}

// greyPanelBlankRow renders one empty row using the shared grey panel style.
func greyPanelBlankRow(width int) string {
	return greyPanelBackground + strings.Repeat(" ", width) + colorReset
}

// greyPanelRow renders one padded row using the shared grey panel style.
func greyPanelRow(width int, segments ...greyPanelSegment) string {
	contentWidth := 0
	for _, segment := range segments {
		contentWidth += visibleWidth(segment.text)
	}

	rightPadding := width - greyPanelHorizontalPadding - contentWidth
	if rightPadding < greyPanelHorizontalPadding {
		rightPadding = greyPanelHorizontalPadding
	}

	var b strings.Builder
	b.WriteString(greyPanelBackground)
	b.WriteString(strings.Repeat(" ", greyPanelHorizontalPadding))
	for _, segment := range segments {
		if segment.color != "" {
			b.WriteString(segment.color)
		} else {
			b.WriteString(greyPanelForeground)
		}
		b.WriteString(segment.text)
	}
	b.WriteString(greyPanelForeground)
	b.WriteString(strings.Repeat(" ", rightPadding))
	b.WriteString(colorReset)
	return b.String()
}

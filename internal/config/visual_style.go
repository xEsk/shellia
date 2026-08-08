package config

import "strings"

// VisualStyle identifies the terminal structure used to present Shellia output.
type VisualStyle string

const (
	VisualStylePlain VisualStyle = "plain"
	VisualStyleGuide VisualStyle = "guide"
	VisualStyleBands VisualStyle = "bands"
	VisualStyleCards VisualStyle = "cards"
)

// visualStyles returns every selectable visual style in menu order.
func visualStyles() []VisualStyle {
	return []VisualStyle{
		VisualStylePlain,
		VisualStyleGuide,
		VisualStyleBands,
		VisualStyleCards,
	}
}

// normalizeVisualStyle validates the configured terminal visual style.
func normalizeVisualStyle(value string, fallback VisualStyle) VisualStyle {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(VisualStylePlain):
		return VisualStylePlain
	case string(VisualStyleGuide):
		return VisualStyleGuide
	case string(VisualStyleBands):
		return VisualStyleBands
	case string(VisualStyleCards):
		return VisualStyleCards
	default:
		return fallback
	}
}

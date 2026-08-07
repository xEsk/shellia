package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

type stepSurface interface {
	writer() io.Writer
	ansiEnabled() bool
	contentWidth() int
	writeRow(string)
	replaceLastRenderedRow(string)
	renderEditableRow(string, int)
	close()
}

type rowSurfaceSpec struct {
	target      io.Writer
	ansi        bool
	width       int
	prefix      string
	closingRows []string
}

type rowSurface struct {
	rowSurfaceSpec
	closed bool
}

func newRowSurface(spec rowSurfaceSpec) *rowSurface {
	if spec.target == nil {
		spec.target = io.Discard
	}
	if spec.width < 1 {
		spec.width = 1
	}
	return &rowSurface{rowSurfaceSpec: spec}
}

func (surface *rowSurface) writer() io.Writer {
	return surface.target
}

func (surface *rowSurface) ansiEnabled() bool {
	return surface.ansi
}

func (surface *rowSurface) contentWidth() int {
	return surface.width
}

func (surface *rowSurface) writeRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}
	fmt.Fprintln(surface.target, surface.prefix+rendered)
}

func (surface *rowSurface) replaceLastRenderedRow(rendered string) {
	if surface == nil || surface.closed {
		return
	}

	output, ok := surface.target.(*os.File)
	if !ok || !term.IsTerminal(int(output.Fd())) {
		surface.writeRow(rendered)
		return
	}

	fmt.Fprint(surface.target, "\033[1A\r\033[2K")
	surface.writeRow(rendered)
}

func (surface *rowSurface) renderEditableRow(rendered string, moveLeft int) {
	if surface == nil || surface.closed {
		return
	}
	fmt.Fprint(surface.target, "\r\033[K", surface.prefix, rendered)
	if moveLeft > 0 {
		fmt.Fprintf(surface.target, "\033[%dD", moveLeft)
	}
}

func (surface *rowSurface) close() {
	if surface == nil || surface.closed {
		return
	}
	for _, row := range surface.closingRows {
		fmt.Fprintln(surface.target, row)
	}
	surface.closed = true
}

func surfaceContentWidth(target io.Writer, prefix string) int {
	width := boxWidthFor(target) - visibleWidth(prefix)
	if width < 1 {
		return 1
	}
	return width
}

// wrapPlainText splits plain text into lines of a fixed visible width, respecting word boundaries.
func wrapPlainText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	if text == "" {
		return []string{""}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0)
	current := words[0]

	for _, word := range words[1:] {
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			runes := []rune(word)
			for len(runes) > width {
				lines = append(lines, string(runes[:width]))
				runes = runes[width:]
			}
			current = string(runes)
		}
	}
	lines = append(lines, current)
	return lines
}

// wrapRenderedRows splits rendered text without breaking ANSI sequences and
// carries active styling across continuation rows.
func wrapRenderedRows(rendered string, width int) []string {
	if width < 1 {
		width = 1
	}
	if visibleWidth(rendered) <= width {
		return []string{rendered}
	}

	lines := make([]string, 0, (visibleWidth(rendered)/width)+1)
	var current strings.Builder
	visible := 0
	activeStyle := ""
	for offset := 0; offset < len(rendered); {
		if sequence, size := renderedANSISequence(rendered[offset:]); size > 0 {
			current.WriteString(sequence)
			if strings.HasSuffix(sequence, "m") {
				if sequence == colorReset || sequence == "\033[m" {
					activeStyle = ""
				} else {
					activeStyle += sequence
				}
			}
			offset += size
			continue
		}

		_, size := utf8.DecodeRuneInString(rendered[offset:])
		if size == 0 {
			break
		}
		if visible == width {
			if activeStyle != "" {
				current.WriteString(colorReset)
			}
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(activeStyle)
			visible = 0
		}
		current.WriteString(rendered[offset : offset+size])
		visible++
		offset += size
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func renderedANSISequence(text string) (string, int) {
	if len(text) < 3 || text[0] != '\033' || text[1] != '[' {
		return "", 0
	}
	for index := 2; index < len(text); index++ {
		if text[index] >= '@' && text[index] <= '~' {
			return text[:index+1], index + 1
		}
	}
	return "", 0
}

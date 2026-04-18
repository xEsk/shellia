package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// stepBox renders a full step with its state, confirmation, and output.
type stepBox struct {
	target        io.Writer
	ui            bool
	width         int
	outputStarted bool
	closed        bool
}

// newStepBox creates and opens a new step box with a consistent width.
func newStepBox(target io.Writer, ui bool, title string) *stepBox {
	box := &stepBox{
		target: target,
		ui:     ui,
		width:  boxWidth(),
	}
	fmt.Fprintln(target)
	fmt.Fprintln(target, box.separatorLine())
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(box.ui, colorMagenta+colorBold, title))
	return box
}

// Close closes the step box if it is still open.
func (box *stepBox) Close() {
	if box == nil || box.closed {
		return
	}
	fmt.Fprintln(box.target)
	box.closed = true
}

// Spacer adds a blank line inside the box.
func (box *stepBox) Spacer() {
	box.writeRow("")
}

// Bullet prints a main line for the step.
func (box *stepBox) Bullet(text string) {
	box.writePrefixed("• ", style(box.ui, colorWhite+colorBold, "• "), text, colorWhite+colorBold)
}

// Text prints a text line with simple indentation inside the box.
func (box *stepBox) Text(text string, color string) {
	box.writePrefixed("", "", text, color)
}

// Section prints a short section inside the box with a colour accent.
func (box *stepBox) Section(label string, color string) {
	box.writePrefixed("• ", style(box.ui, color+colorBold, "• "), label, color)
}

// KeyValue prints a line with a label and value, wrapping when needed.
func (box *stepBox) KeyValue(label string, value string, labelColor string, valueColor string) {
	box.writePrefixed("• "+label+" ", style(box.ui, labelColor+colorBold, "• "+label+" "), value, valueColor)
}

// ReplaceLastRenderedRow repaints the last line of the box with already rendered content.
func (box *stepBox) ReplaceLastRenderedRow(rendered string) {
	if box == nil {
		return
	}

	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(box.target, rendered)
		return
	}

	fmt.Fprint(box.target, "\033[1A\r\033[2K")
	fmt.Fprintln(box.target, rendered)
}

// OutputLabel starts the system output section inside the box.
func (box *stepBox) OutputLabel() {
	if box.outputStarted {
		return
	}
	box.Section("system output", colorDim)
	box.Spacer()
	box.outputStarted = true
}

// OutputLine prints a line of system output with leading padding and a subdued colour.
func (box *stepBox) OutputLine(text string) {
	fmt.Fprintln(box.target, "  "+style(box.ui, colorDim, text))
}

// EditCommand shows an editable line inside the box to adjust the proposed command.
func (box *stepBox) EditCommand(reader *bufio.Reader, initial string) (string, error) {
	if box == nil {
		return strings.TrimSpace(initial), nil
	}

	box.Spacer()
	prefixPlain := "• edit "
	prefixRendered := style(box.ui, colorYellow+colorBold, "• edit ")

	fd := int(os.Stdin.Fd())
	buffer := []rune(initial)
	cursor := len(buffer)

	if !term.IsTerminal(fd) {
		box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
		fmt.Fprint(box.target, "\r\n")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		edited := strings.TrimSpace(line)
		if edited == "" {
			return strings.TrimSpace(initial), nil
		}
		return edited, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprint(box.target, "\r\n")
		return strings.TrimSpace(initial), nil
	}
	defer term.Restore(fd, state) //nolint:errcheck

	single := []byte{0}
	box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
	for {
		_, err := os.Stdin.Read(single)
		if err != nil {
			fmt.Fprint(box.target, "\r\n")
			return "", err
		}

		switch single[0] {
		case '\r', '\n':
			fmt.Fprint(box.target, "\r\n")
			return strings.TrimSpace(string(buffer)), nil
		case 3:
			fmt.Fprint(box.target, "\r\n")
			return "", nil
		case 27:
			if err := applyEscapeSequence(&buffer, &cursor, 0, nil); err != nil {
				fmt.Fprint(box.target, "\r\n")
				return "", err
			}
		case 127, 8:
			if cursor == 0 || len(buffer) == 0 {
				continue
			}
			buffer = append(buffer[:cursor-1], buffer[cursor:]...)
			cursor--
		default:
			r, err := readInputRune(single[0])
			if err != nil {
				if errors.Is(err, errDiscardRune) {
					continue
				}
				fmt.Fprint(box.target, "\r\n")
				return "", err
			}
			buffer = append(buffer[:cursor], append([]rune{r}, buffer[cursor:]...)...)
			cursor++
		}

		box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
	}
}

// separatorLine renders a long, subdued line to separate blocks.
func (box *stepBox) separatorLine() string {
	return style(box.ui, colorDim, strings.Repeat("─", box.width))
}

// writePrefixed prints content with a fixed prefix and hard wrapping inside the box.
func (box *stepBox) writePrefixed(prefixPlain string, prefixRendered string, text string, textColor string) {
	available := box.innerWidth() - visibleWidth(prefixPlain)
	if available < 1 {
		available = 1
	}

	lines := wrapPlainText(text, available)
	if len(lines) == 0 {
		lines = []string{""}
	}

	for index, line := range lines {
		if index == 0 {
			box.writeRow(prefixRendered + style(box.ui, textColor, line))
			continue
		}
		padding := strings.Repeat(" ", visibleWidth(prefixPlain))
		box.writeRow(style(box.ui, colorDim, padding) + style(box.ui, textColor, line))
	}
}

// writeRow writes a row inside the box.
func (box *stepBox) writeRow(rendered string) {
	fmt.Fprintln(box.target, rendered)
}

// renderEditableRow repaints a single editable row while keeping the cursor inside the box.
func (box *stepBox) renderEditableRow(prefixPlain string, prefixRendered string, buffer []rune, cursor int) {
	available := box.innerWidth() - visibleWidth(prefixPlain)
	if available < 1 {
		available = 1
	}

	start := 0
	if len(buffer) > available {
		start = cursor - available
		if start < 0 {
			start = 0
		}
		maxStart := len(buffer) - available
		if start > maxStart {
			start = maxStart
		}
	}

	end := start + available
	if end > len(buffer) {
		end = len(buffer)
	}

	view := buffer[start:end]
	cursorInView := cursor - start
	if cursorInView < 0 {
		cursorInView = 0
	}
	if cursorInView > len(view) {
		cursorInView = len(view)
	}

	row := prefixRendered + style(box.ui, colorWhite, string(view))

	fmt.Fprint(box.target, "\r\033[K")
	fmt.Fprint(box.target, row)

	moveLeft := len(view) - cursorInView
	if moveLeft > 0 {
		fmt.Fprintf(box.target, "\033[%dD", moveLeft)
	}
}

// innerWidth returns the usable space between the box borders.
func (box *stepBox) innerWidth() int {
	return box.width
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
			// Hard-break words longer than width.
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

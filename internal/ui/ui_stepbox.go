package ui

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
	surface       stepSurface
	target        io.Writer
	ui            bool
	width         int
	outputStarted bool
	closed        bool
}

// newStepBox creates and opens a new step box with a consistent width.
func newStepBox(target io.Writer, ui bool, title string) *stepBox {
	return newPlainStepBox(target, ui, title)
}

func newStepBoxForSurface(surface stepSurface) *stepBox {
	if surface == nil {
		return nil
	}
	return &stepBox{
		surface: surface,
		target:  surface.writer(),
		ui:      surface.ansiEnabled(),
		width:   surface.contentWidth(),
	}
}

// Close closes the step box if it is still open.
func (box *stepBox) Close() {
	if box == nil || box.closed {
		return
	}
	box.surface.close()
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

	box.surface.replaceLastRenderedRow(rendered)
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
	box.surface.writeRow("  " + style(box.ui, colorDim, text))
}

// EditCommand shows an editable line inside the box to adjust the proposed command.
func (box *stepBox) EditCommand(reader *bufio.Reader, stdin *os.File, initial string) (string, error) {
	if box == nil {
		return strings.TrimSpace(initial), nil
	}
	if stdin == nil {
		stdin = os.Stdin
	}

	box.Spacer()
	prefixPlain := "• edit "
	prefixRendered := style(box.ui, colorYellow+colorBold, "• edit ")

	fd := int(stdin.Fd())
	buffer := []rune(initial)
	cursor := len(buffer)

	if !term.IsTerminal(fd) {
		box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
		fmt.Fprint(box.target, "\r\n")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("cannot read edited command: %w", err)
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
	defer term.Restore(fd, state) //nolint:errcheck // best-effort terminal restore before returning to normal input.

	single := []byte{0}
	box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
	for {
		_, err := stdin.Read(single)
		if err != nil {
			fmt.Fprint(box.target, "\r\n")
			return "", fmt.Errorf("cannot read command editor input: %w", err)
		}
		if err := rawTurnPromptCancellation(single[0]); err != nil {
			fmt.Fprint(box.target, "\r\n")
			return "", err
		}

		switch single[0] {
		case '\r', '\n':
			fmt.Fprint(box.target, "\r\n")
			return strings.TrimSpace(string(buffer)), nil
		case 27:
			if err := applyEscapeSequenceFrom(stdin, &buffer, &cursor, 0, nil); err != nil {
				fmt.Fprint(box.target, "\r\n")
				return "", fmt.Errorf("cannot apply command editor escape sequence: %w", err)
			}
		case 127, 8:
			if cursor == 0 || len(buffer) == 0 {
				continue
			}
			buffer = append(buffer[:cursor-1], buffer[cursor:]...)
			cursor--
		default:
			r, err := readInputRuneFrom(stdin, single[0])
			if err != nil {
				if errors.Is(err, errDiscardRune) {
					continue
				}
				fmt.Fprint(box.target, "\r\n")
				return "", fmt.Errorf("cannot decode command editor input: %w", err)
			}
			buffer = append(buffer[:cursor], append([]rune{r}, buffer[cursor:]...)...)
			cursor++
		}

		box.renderEditableRow(prefixPlain, prefixRendered, buffer, cursor)
	}
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
	box.surface.writeRow(rendered)
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

	moveLeft := len(view) - cursorInView
	box.surface.renderEditableRow(row, moveLeft)
}

// innerWidth returns the usable space between the box borders.
func (box *stepBox) innerWidth() int {
	return box.width
}

package executor

import (
	"fmt"
	"io"
	"strings"
)

type prefixedWriter struct {
	box     *stepBox
	hidden  bool
	started bool
	buffer  string
}

type directShellWriter struct {
	ui        bool
	target    io.Writer
	lineStart bool
	started   bool
}

// Write applies a visual prefix to each line of shell output.
func (writer *prefixedWriter) Write(data []byte) (int, error) {
	if writer.hidden {
		return len(data), nil
	}

	writer.buffer += string(data)
	for len(writer.buffer) > 0 {
		if !writer.started {
			if writer.box != nil {
				writer.box.OutputLabel()
			}
			writer.started = true
		}

		newlineIndex := strings.IndexByte(writer.buffer, '\n')
		if newlineIndex == -1 {
			return len(data), nil
		}

		chunk := strings.TrimSuffix(writer.buffer[:newlineIndex+1], "\n")
		if writer.box != nil {
			writer.box.OutputLine(chunk)
		}
		writer.buffer = writer.buffer[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush forces printing of the last partial line in the buffer when present.
func (writer *prefixedWriter) Flush() error {
	if writer.hidden {
		return nil
	}

	if !writer.started || writer.buffer == "" {
		return nil
	}
	if writer.box != nil {
		writer.box.OutputLine(writer.buffer)
	}
	writer.buffer = ""
	return nil
}

// Write prints /shell mode output directly with a more subdued tone.
func (writer *directShellWriter) Write(data []byte) (int, error) {
	text := string(data)
	for len(text) > 0 {
		if writer.lineStart {
			if !writer.started {
				if _, err := fmt.Fprint(writer.target, "\n"); err != nil {
					return 0, err
				}
				writer.started = true
			}
			if _, err := fmt.Fprint(writer.target, styleStart(writer.ui, colorDim)); err != nil {
				return 0, err
			}
			writer.lineStart = false
		}

		newlineIndex := strings.IndexByte(text, '\n')
		if newlineIndex == -1 {
			if _, err := fmt.Fprint(writer.target, text); err != nil {
				return 0, err
			}
			return len(data), nil
		}

		chunk := text[:newlineIndex]
		if _, err := fmt.Fprint(writer.target, chunk, styleEnd(writer.ui), "\n"); err != nil {
			return 0, err
		}
		writer.lineStart = true
		text = text[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush closes the last partial line of /shell mode when present.
func (writer *directShellWriter) Flush() error {
	if writer.lineStart {
		return nil
	}
	_, err := fmt.Fprint(writer.target, styleEnd(writer.ui))
	return err
}

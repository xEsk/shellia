package executor

import (
	"fmt"
	"io"
	"strings"
)

type prefixedWriter struct {
	box     StepPresenter
	hidden  bool
	started bool
	buffer  string
}

type directShellWriter struct {
	presenter Presenter
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
		newlineIndex := strings.IndexByte(writer.buffer, '\n')
		if newlineIndex == -1 {
			return len(data), nil
		}

		if !writer.started {
			if writer.box != nil {
				writer.box.OutputLabel()
			}
			writer.started = true
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

	if writer.buffer == "" {
		return nil
	}
	if !writer.started {
		if writer.box != nil {
			writer.box.OutputLabel()
		}
		writer.started = true
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
			if _, err := fmt.Fprint(writer.target, writer.presenter.StyleStart(ToneDim)); err != nil {
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
		if _, err := fmt.Fprint(writer.target, chunk, writer.presenter.StyleEnd(), "\n"); err != nil {
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
	_, err := fmt.Fprint(writer.target, writer.presenter.StyleEnd())
	return err
}

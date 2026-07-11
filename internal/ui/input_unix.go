package ui

import "golang.org/x/sys/unix"

// escapeSequencePollMs is the poll window to distinguish a bare Esc from an escape sequence.
const escapeSequencePollMs = 25

// isInputReady reports whether stdin has bytes pending within escapeSequencePollMs.
// Used to distinguish a bare Esc keypress from the start of an escape sequence.
func isInputReady(fd int) (bool, error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, escapeSequencePollMs)
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	return poll[0].Revents&unix.POLLIN != 0, nil
}

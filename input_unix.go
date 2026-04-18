package main

import "golang.org/x/sys/unix"

// isInputReady reports whether stdin has bytes pending within 25 ms.
// Used to distinguish a bare Esc keypress from the start of an escape sequence.
func isInputReady(fd int) (bool, error) {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 25)
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	return poll[0].Revents&unix.POLLIN != 0, nil
}

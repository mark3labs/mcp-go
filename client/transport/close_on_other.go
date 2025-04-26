//go:build !windows
// +build !windows

package transport

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

// killprocess kills the process on non-windows platforms.
func killProcess(proc *os.Process) error {
	err := proc.Signal(syscall.SIGTERM)
	if err != nil {
		fmt.Printf("Failed to send SIGTERM to pid %d: %v\n", proc.Pid, err)
	}
	// wait for a short time to allow the process to terminate gracefully
	time.Sleep(200 * time.Millisecond)
	// check if the process is still running
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		// check if the process is gone
		// on some platforms, this may return "no such process" if the process is already gone
		if strings.Contains(err.Error(), "no such process") {
			return nil
		}
	}

	// if the process is still running, kill it
	return proc.Kill()
}

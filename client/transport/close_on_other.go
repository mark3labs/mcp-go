//go:build !windows
// +build !windows

package transport

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/process"
)

func killByPid(pid int) error {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return err
	}
	// first send SIGTERM
	err = proc.Terminate()
	if err != nil {
		fmt.Printf("Failed to send SIGTERM to pid %d: %v\n", pid, err)
	}

	_, err = proc.Status()
	if err == nil {
		// kill ok
		return nil
	}
	// send SIGKILL
	return proc.Kill()
}

//go:build windows
// +build windows

package transport

import (
	"fmt"
	"os"

	"github.com/shirou/gopsutil/v3/process"
)

func killByPid(pid int) error {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return err
	}
	// get all subprocess recursively
	children, err := proc.Children()
	if err == nil {
		for _, child := range children {
			err = killByPid(int(child.Pid)) // kill all subprocesses
			if err != nil {
				fmt.Printf("Failed to kill pid %d: %v\n", child.Pid, err)
			}
		}
	}

	// kill current process
	p, err := os.FindProcess(int(pid))
	if err == nil {
		// windows does not support SIGTERM, so we just use Kill()
		err = p.Kill()
		if err != nil {
			fmt.Printf("Failed to kill pid %d: %v\n", pid, err)
		}
	}
	return err
}

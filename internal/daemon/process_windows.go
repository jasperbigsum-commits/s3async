//go:build windows

package daemon

import "os"

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if proc == nil {
		return false
	}
	return true
}

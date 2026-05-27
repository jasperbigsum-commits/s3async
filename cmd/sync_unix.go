// +build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

// configSysProcAttr configures Unix-specific process attributes.
func configSysProcAttr(proc *exec.Cmd) {
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

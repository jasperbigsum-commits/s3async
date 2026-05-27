// +build windows

package cmd

import (
	"os/exec"
)

// configSysProcAttr configures Windows-specific process attributes.
// On Windows, Setpgid is not supported, so we do nothing.
func configSysProcAttr(proc *exec.Cmd) {
	// No-op on Windows
}

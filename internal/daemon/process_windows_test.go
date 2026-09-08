//go:build windows

package daemon

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProcessAliveWindows(t *testing.T) {
	if processAlive(0) || processAlive(-1) {
		t.Fatal("invalid PID reported alive")
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("current process reported dead")
	}
	child := exec.Command(os.Args[0], "-test.run=^TestProcessAliveHelper$")
	child.Env = append(os.Environ(), "S3ASYNC_PROCESS_TEST_HELPER=1")
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	if !processAlive(child.Process.Pid) {
		t.Fatal("running child reported dead")
	}
	// Keep the process object available even after exit, to catch existence-only checks.
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(child.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if processAlive(child.Process.Pid) {
		t.Fatal("exited child reported alive")
	}
}

func TestProcessAliveHelper(t *testing.T) {
	if os.Getenv("S3ASYNC_PROCESS_TEST_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

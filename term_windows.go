package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// enableVirtualTerminal turns on VT escape-sequence processing for the console output.
func enableVirtualTerminal() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}

// shellCommand runs line through cmd.exe, the way Node's `shell: true` does on Windows.
func shellCommand(line string) *exec.Cmd {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	c := exec.Command(comspec)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/d /s /c "` + line + `"`}
	return c
}

//go:build !windows

package main

import "os/exec"

func enableVirtualTerminal() {}

// shellCommand runs line through /bin/sh.
func shellCommand(line string) *exec.Cmd { return exec.Command("/bin/sh", "-c", line) }

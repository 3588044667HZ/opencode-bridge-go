//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

func Prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

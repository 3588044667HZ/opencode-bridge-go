//go:build windows

package process

import (
	"os/exec"
	"strconv"
)

func prepareCommand(cmd *exec.Cmd) {}

func killTree(pid int) {
	_ = exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

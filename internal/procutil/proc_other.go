//go:build !windows

package procutil

import "os/exec"

func Prepare(cmd *exec.Cmd) {
}

//go:build !windows

package bridge

import (
	"errors"
	"strings"
	"syscall"
)

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

//go:build windows

package bridge

import (
	"errors"
	"strings"
	"syscall"
)

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.Errno(10048)) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "only one usage of each socket address")
}

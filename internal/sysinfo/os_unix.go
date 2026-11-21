//go:build !windows

package sysinfo

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func osType() string {
	var info unix.Utsname
	if err := unix.Uname(&info); err == nil {
		if value := unix.ByteSliceToString(info.Sysname[:]); value != "" {
			return value
		}
	}
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "Darwin"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	case "dragonfly":
		return "DragonFly"
	case "solaris":
		return "SunOS"
	default:
		return runtime.GOOS
	}
}

func osRelease() string {
	var info unix.Utsname
	if err := unix.Uname(&info); err == nil {
		return unix.ByteSliceToString(info.Release[:])
	}
	return ""
}

func osVersion() string {
	var info unix.Utsname
	if err := unix.Uname(&info); err == nil {
		return unix.ByteSliceToString(info.Version[:])
	}
	return ""
}

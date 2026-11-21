//go:build windows

package files

import (
	"os"
	"syscall"
	"time"
)

func createdAt(info os.FileInfo) time.Time {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, data.CreationTime.Nanoseconds())
	}
	return info.ModTime()
}

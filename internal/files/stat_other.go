//go:build !windows

package files

import (
	"os"
	"time"
)

func createdAt(info os.FileInfo) time.Time {
	return info.ModTime()
}

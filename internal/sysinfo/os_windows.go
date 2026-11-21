//go:build windows

package sysinfo

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func osType() string {
	return "Windows_NT"
}

func osRelease() string {
	info := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", info.MajorVersion, info.MinorVersion, info.BuildNumber)
}

func osVersion() string {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err == nil {
		defer key.Close()
		if product, _, valueErr := key.GetStringValue("ProductName"); valueErr == nil && strings.TrimSpace(product) != "" {
			return product
		}
	}
	info := windows.RtlGetVersion()
	if version := strings.TrimSpace(windows.UTF16ToString(info.CsdVersion[:])); version != "" {
		return version
	}
	return fmt.Sprintf("Windows %d.%d", info.MajorVersion, info.MinorVersion)
}

//go:build windows

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diskUsage(path string) (uint64, uint64) {
	if path == "" {
		path = "."
	}
	if _, err := os.Stat(path); err != nil {
		path = "."
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	pathPtr, err := windows.UTF16PtrFromString(absPath)
	if err != nil {
		return 0, 0
	}
	var freeBytesAvailable uint64
	var totalBytes uint64
	var totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return 0, 0
	}
	if totalFreeBytes > totalBytes {
		return 0, totalBytes
	}
	return totalBytes - totalFreeBytes, totalBytes
}

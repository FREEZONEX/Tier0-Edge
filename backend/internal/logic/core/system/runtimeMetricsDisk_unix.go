//go:build !windows

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"reflect"
	"syscall"
)

func diskUsage(path string) (uint64, uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		if err := syscall.Statfs("/", &stat); err != nil {
			return 0, 0
		}
	}
	blockSize := statBlockSize(stat)
	total := stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	if free > total {
		return 0, total
	}
	return total - free, total
}

func statBlockSize(stat syscall.Statfs_t) uint64 {
	blockSize := uint64(stat.Bsize)
	frsize := reflect.ValueOf(stat).FieldByName("Frsize")
	if !frsize.IsValid() {
		return blockSize
	}
	switch frsize.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value := frsize.Int(); value > 0 {
			return uint64(value)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value := frsize.Uint(); value > 0 {
			return value
		}
	}
	return blockSize
}

package nativeui

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

// 默认选择可写且有足够空间的非 C 固定磁盘；用户仍可编辑完整目录。
func DefaultInstallDirectory(required uint64) (string, string) {
	drives, _, _ := kernel32.NewProc("GetLogicalDrives").Call()
	order := []byte{'D'}
	for letter := byte('A'); letter <= 'Z'; letter++ {
		if letter != 'C' && letter != 'D' {
			order = append(order, letter)
		}
	}
	for _, letter := range order {
		if drives&(1<<uint(letter-'A')) == 0 {
			continue
		}
		root := string(letter) + `:\`
		if usableDrive(root, required) {
			return unusedDirectory(filepath.Join(root, "Tier0NativePoC")), "默认选择可写的非 C 磁盘。可修改下方安装目录。"
		}
	}
	return unusedDirectory(filepath.Join(os.Getenv("LOCALAPPDATA"), "Tier0NativePoC")), "未找到空间充足且可写的非 C 固定磁盘，默认使用当前用户目录。可自行更改。"
}
func usableDrive(root string, required uint64) bool {
	p := textPointer(root)
	kind, _, _ := kernel32.NewProc("GetDriveTypeW").Call(uintptr(unsafe.Pointer(p)))
	if kind != 3 {
		return false
	}
	var flags uint32
	ok, _, _ := kernel32.NewProc("GetVolumeInformationW").Call(uintptr(unsafe.Pointer(p)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&flags)), 0, 0)
	if ok == 0 || flags&8 == 0 {
		return false
	}
	var available uint64
	ok, _, _ = kernel32.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&available)), 0, 0)
	if ok == 0 || available < required+(512<<20) {
		return false
	}
	f, err := os.CreateTemp(root, ".tier0-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	err = f.Close()
	removeErr := os.Remove(name)
	return err == nil && removeErr == nil
}
func unusedDirectory(base string) string {
	for i := 0; i < 100; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return base
}

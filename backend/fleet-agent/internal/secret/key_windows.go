//go:build windows

package secret

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func loadOrCreatePlatformKey(path string) ([]byte, error) {
	protected, err := os.ReadFile(path)
	if err == nil {
		return unprotectWindowsKey(protected)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := generateMasterKey()
	if err != nil {
		return nil, err
	}
	protected, err = protectWindowsKey(key)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreatePlatformKey(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(protected); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

func protectWindowsKey(key []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(key)), Data: &key[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN | windows.CRYPTPROTECT_LOCAL_MACHINE)
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, flags, &out); err != nil {
		return nil, fmt.Errorf("protect master key with Windows DPAPI: %w", err)
	}
	return copyAndFreeWindowsBlob(out)
}

func unprotectWindowsKey(protected []byte) ([]byte, error) {
	if len(protected) == 0 {
		return nil, errors.New("Windows DPAPI master key file is empty")
	}
	in := windows.DataBlob{Size: uint32(len(protected)), Data: &protected[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, flags, &out); err != nil {
		return nil, fmt.Errorf("unprotect master key with Windows DPAPI: %w", err)
	}
	return copyAndFreeWindowsBlob(out)
}

func copyAndFreeWindowsBlob(blob windows.DataBlob) ([]byte, error) {
	if blob.Data == nil || blob.Size == 0 {
		return nil, errors.New("Windows DPAPI returned an empty value")
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(blob.Data))))
	value := unsafe.Slice(blob.Data, int(blob.Size))
	return append([]byte(nil), value...), nil
}

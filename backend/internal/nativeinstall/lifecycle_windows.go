package nativeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func SafePath(path string) error {
	path, err := filepath.Abs(path)
	if err != nil || strings.HasPrefix(path, `\\`) {
		return errors.New("安装操作仅支持本机绝对路径")
	}
	for p := path; ; p = filepath.Dir(p) {
		name, e := windows.UTF16PtrFromString(p)
		if e != nil {
			return e
		}
		attrs, e := windows.GetFileAttributes(name)
		if e == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("安装路径不能包含链接或重解析点：%s", p)
		}
		if e != nil && !errors.Is(e, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(e, windows.ERROR_PATH_NOT_FOUND) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

func Lock(root string) (func(), error) {
	sum := sha256.Sum256([]byte(strings.ToLower(root)))
	name, _ := windows.UTF16PtrFromString(`Local\Tier0NativeUpdate-` + hex.EncodeToString(sum[:6]))
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, fmt.Errorf("此安装正在安装、更新或卸载，或无法加锁：%w", err)
	}
	return func() { windows.CloseHandle(h) }, nil
}

// Enumerate and revalidate process handles by executable path, never by name or
// a stale PID from disk. Only a stopped supervisor is terminated to close its tray.
func installationProcesses(root string, closeTray bool) (int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(snapshot, &entry)
	count := 0
	for err == nil {
		h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if e == nil {
			buf := make([]uint16, 32768)
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
				path := windows.UTF16ToString(buf[:n])
				rel, e := filepath.Rel(root, path)
				if e == nil && filepath.IsLocal(rel) {
					count++
					if closeTray && strings.EqualFold(rel, "Start.exe") {
						if e := windows.TerminateProcess(h, 0); e != nil {
							windows.CloseHandle(h)
							return count, e
						}
						windows.WaitForSingleObject(h, 5000)
					}
				}
			}
			windows.CloseHandle(h)
		}
		err = windows.Process32Next(snapshot, &entry)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return count, err
	}
	return count, nil
}
func Stop(root string) error {
	if err := os.WriteFile(filepath.Join(root, ".installing"), []byte("Update in progress; do not start."), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "stop.request"), []byte("stop"), 0600); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(root)))
	name, _ := windows.UTF16PtrFromString(`Local\Tier0Native-` + hex.EncodeToString(sum[:6]))
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
		if h != 0 {
			windows.CloseHandle(h)
		}
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			if _, err := installationProcesses(root, true); err != nil {
				return err
			}
			n, err := installationProcesses(root, false)
			if err != nil {
				return err
			}
			if n == 0 {
				if _, err := os.Stat(filepath.Join(root, "data", "postgres", "postmaster.pid")); !os.IsNotExist(err) {
					return errors.New("数据库尚未正常关闭，保留原目录，不执行切换")
				}
				return nil
			}
		} else if err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("等待本套服务停止超时，未覆盖数据")
}
func RemoveMarker(root string) error {
	for _, name := range []string{".installing", "stop.request"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

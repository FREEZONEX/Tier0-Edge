package nativeinstall

import (
	"backend/internal/runtimeenv"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const RetainedMarker = ".tier0-uninstalled"

func registrationPath(root string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return `Software\Microsoft\Windows\CurrentVersion\Uninstall\Tier0Native-` + hex.EncodeToString(sum[:12])
}

// Register is called only for the final installation, never an upgrade stage.
func Register(root string) error {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return err
	}
	var m struct {
		PackageVersion string                  `json:"packageVersion"`
		Distribution   runtimeenv.Distribution `json:"distribution"`
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return err
	}
	identity, err := runtimeenv.ReadArtifactIdentity(filepath.Join(root, "Uninstall.exe"))
	if err != nil {
		return err
	}
	if identity != m.Distribution.Identity || identity != runtimeenv.Identity() || !runtimeenv.ValidPackageVersion(m.Distribution.ProductVersion, m.PackageVersion) {
		return errors.New("卸载登记与安装身份不匹配")
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, registrationPath(root), registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	for name, value := range map[string]string{
		"DisplayName": runtimeenv.ProductName(), "DisplayVersion": m.PackageVersion, "Publisher": "Tier0",
		"InstallLocation": root, "DisplayIcon": filepath.Join(root, "tier0.ico"),
		"UninstallString": `"` + filepath.Join(root, "Uninstall.exe") + `"`,
	} {
		if err = key.SetStringValue(name, value); err != nil {
			return err
		}
	}
	if err = key.SetDWordValue("NoModify", 1); err != nil {
		return err
	}
	return key.SetDWordValue("NoRepair", 1)
}

// An update recovery may restore a package that predates the uninstaller.
func RestoreRegistration(root string) error {
	if _, err := os.Stat(filepath.Join(root, "Uninstall.exe")); os.IsNotExist(err) {
		return unregister(root)
	} else if err != nil {
		return err
	}
	return Register(root)
}

func unregister(root string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registrationPath(root), registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	location, _, err := key.GetStringValue("InstallLocation")
	key.Close()
	if err != nil {
		return err
	}
	if !strings.EqualFold(location, root) {
		return errors.New("卸载登记的安装目录不匹配")
	}
	return registry.DeleteKey(registry.CURRENT_USER, registrationPath(root))
}

// Only inventory-owned program files may be removed. Never traverse data or
// recursively delete directories: unknown user files must survive uninstall.
func programFile(name string) bool {
	name = filepath.FromSlash(name)
	if !filepath.IsLocal(name) || strings.ContainsAny(name, ":\x00") || filepath.Clean(name) != name {
		return false
	}
	top := strings.ToLower(strings.Split(name, string(filepath.Separator))[0])
	switch top {
	case "backend", "postgres", "node", "nats", "opcua", "mqtt", "web", "licenses":
		return strings.ContainsRune(name, filepath.Separator)
	case "start.exe", "uninstall.exe", "stop.cmd", "tier0.ico", "readme.txt", "validation.md", "network.md", "size-report.json", "package.json":
		return top == strings.ToLower(name)
	}
	return false
}

func uninstallFiles(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, "FILES.txt"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var files []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		size, name, ok := strings.Cut(scanner.Text(), "  ")
		if _, err := strconv.ParseUint(size, 10, 64); err != nil || !ok || !programFile(name) {
			return nil, fmt.Errorf("不安全的程序清单项：%q", name)
		}
		name = filepath.FromSlash(name)
		if seen[strings.ToLower(name)] {
			return nil, errors.New("程序清单包含重复路径")
		}
		seen[strings.ToLower(name)] = true
		if err := SafePath(filepath.Join(root, name)); err != nil {
			return nil, err
		}
		if info, err := os.Lstat(filepath.Join(root, name)); err == nil && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("程序清单项不是普通文件：%s", name)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if name != "package.json" && name != "Uninstall.exe" {
			files = append(files, name)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !seen["start.exe"] || !seen["backend\\backend.exe"] || !seen["uninstall.exe"] || !seen["package.json"] {
		return nil, errors.New("程序文件清单不完整")
	}
	return files, nil
}

func UninstallPreflight(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return errors.New("卸载需要完整的本机安装目录")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.EqualFold(home, root) {
		return errors.New("不能卸载用户主目录")
	}
	if err := SafePath(root); err != nil {
		return err
	}
	for _, name := range []string{"package.json", "FILES.txt", "Uninstall.exe"} {
		if err := SafePath(filepath.Join(root, name)); err != nil {
			return err
		}
	}
	journal := filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+".tier0-update.json")
	if _, err := os.Lstat(journal); !os.IsNotExist(err) {
		return errors.New("请先恢复未完成的更新，再卸载")
	}
	distribution, err := runtimeenv.ReadDistribution(root)
	if err != nil {
		return err
	}
	if err = runtimeenv.ValidateDistribution(distribution); err != nil {
		return err
	}
	identity, err := runtimeenv.ReadArtifactIdentity(filepath.Join(root, "Uninstall.exe"))
	if err != nil {
		return err
	}
	if identity != runtimeenv.Identity() {
		return errors.New("卸载程序与本安装身份不匹配")
	}
	_, err = uninstallFiles(root)
	return err
}

func Uninstall(root string) error {
	unlock, err := Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	if err = UninstallPreflight(root); err != nil {
		return err
	}
	files, err := uninstallFiles(root)
	if err != nil {
		return err
	}
	// Stop owns the marker and checks the installation mutex, executable paths
	// and PostgreSQL shutdown. Leave the marker on failure to prevent restart.
	if err = Stop(root); err != nil {
		return err
	}
	if err = removeProgramFiles(root, files); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, RetainedMarker), []byte("Program removed; data, state, logs and package identity retained.\n"), 0600); err != nil {
		return err
	}
	if err = unregister(root); err != nil {
		return err
	}
	// RemoveMarker must succeed before deleting the retry entry point.
	if err = RemoveMarker(root); err != nil {
		return err
	}
	return os.Remove(filepath.Join(root, "Uninstall.exe"))
}

func removeProgramFiles(root string, files []string) error {
	dirs := map[string]bool{}
	for _, name := range files {
		if !programFile(name) || name == "package.json" || name == "Uninstall.exe" {
			return errors.New("invalid program removal plan")
		}
		file := filepath.Join(root, name)
		if err := SafePath(file); err != nil {
			return err
		}
		if info, e := os.Lstat(file); e == nil && info.IsDir() {
			return fmt.Errorf("程序文件已变成目录：%s", name)
		}
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		for dir := filepath.Dir(file); dir != root; dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, dir := range ordered {
		_ = os.Remove(dir)
	} // Only empty directories; user files remain.
	return nil
}

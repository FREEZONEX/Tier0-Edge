//go:build ignore

package main

import (
	"backend/internal/nativeinstall"
	"backend/internal/runtimeenv"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

type nativeManifest struct {
	Distribution   runtimeenv.Distribution `json:"distribution"`
	Edition        string                  `json:"productEdition"`
	Kind           string                  `json:"kind"`
	Platform       string                  `json:"platform"`
	Architecture   string                  `json:"architecture"`
	Revision       string                  `json:"sourceRevision"`
	CreatedAt      time.Time               `json:"createdAt"`
	ProductVersion string                  `json:"productVersion"`
	PackageVersion string                  `json:"packageVersion"`
	Dependencies   []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"dependencies"`
}
type updateRecord struct {
	Root   string `json:"root"`
	Stage  string `json:"stage"`
	Backup string `json:"backup"`
	Failed string `json:"failed"`
	Phase  string `json:"phase"`
}

func updateJournal(root string) string {
	return filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+".tier0-update.json")
}
func updatePending(root string) bool { _, err := os.Lstat(updateJournal(root)); return err == nil }
func readNativeManifest(raw []byte) (nativeManifest, error) {
	var m nativeManifest
	err := json.Unmarshal(raw, &m)
	if err != nil || m.Kind != "windows-native-validation" || m.Platform != "windows" || m.Architecture != "amd64" || m.Revision == "" || m.CreatedAt.IsZero() {
		return m, errors.New("不是完整的 Windows x64 原生安装包")
	}
	return m, nil
}
func updatePreflight(root string) (string, error) {
	if err := nativeinstall.SafePath(root); err != nil {
		return "", err
	}
	if executable, err := os.Executable(); err == nil {
		if rel, err := filepath.Rel(root, executable); err == nil && filepath.IsLocal(rel) {
			return "", errors.New("请把新安装器放在原安装目录之外（例如下载目录）后再更新")
		}
	}
	if updatePending(root) {
		return "", errors.New("上次更新未完成，请先恢复")
	}
	if _, err := os.Stat(filepath.Join(root, ".installing")); err == nil {
		return "", errors.New("此目录安装未完成，不能直接升级")
	}
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", err
	}
	old, err := readNativeManifest(raw)
	if err != nil {
		return "", err
	}
	archive, _, err := archiveInfo()
	if err != nil {
		return "", err
	}
	raw, err = fs.ReadFile(archive, "package.json")
	if err != nil {
		return "", err
	}
	next, err := readNativeManifest(raw)
	if err != nil {
		return "", err
	}
	if err = runtimeenv.ValidateDistribution(next.Distribution); err != nil {
		return "", err
	}
	if !runtimeenv.ValidPackageVersion(next.ProductVersion, next.PackageVersion) || next.ProductVersion != next.Distribution.ProductVersion || next.PackageVersion != runtimeenv.BuildPackageVersion {
		return "", errors.New("安装包版本标识不一致")
	}
	installed, err := runtimeenv.ReadArtifactIdentity(filepath.Join(root, "backend", "backend.exe"))
	if err != nil {
		return "", fmt.Errorf("cannot verify installed product identity: %w", err)
	}
	if installed.Edition != next.Distribution.Edition || installed.Platform != next.Distribution.Platform || (installed.Deployment != "" && installed.Deployment != next.Distribution.Deployment) {
		return "", errors.New("product edition/platform/deployment conversion requires an explicit migration; ordinary upgrade refused")
	}
	if err = checkDataCompatibility(root, old, next); err != nil {
		return "", err
	}
	for _, name := range []string{"Start.exe", "backend/backend.exe", "postgres/bin/pg_ctl.exe"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			return "", fmt.Errorf("已有安装缺少 %s", name)
		}
	}
	for _, f := range archive.File {
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return "", errors.New("安装包路径不安全")
		}
		top := strings.ToLower(strings.Split(strings.ReplaceAll(name, `\`, "/"), "/")[0])
		if top == "data" || top == "state" || top == "logs" || strings.HasPrefix(top, ".") || top == "stop.request" {
			return "", errors.New("安装包混入运行数据，拒绝升级")
		}
	}
	if old.PackageVersion == "" {
		old.PackageVersion = old.ProductVersion + " (" + old.Revision + ")"
	}
	return fmt.Sprintf("当前版本：%s\r\n目标版本：%s\r\n安装目录：%s", old.PackageVersion, next.PackageVersion, root), nil
}
func saveUpdate(r updateRecord) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := updateJournal(r.Root)
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
func treeSize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := nativeinstall.SafePath(path); err != nil {
			return err
		}
		if !d.IsDir() {
			st, err := d.Info()
			if err != nil {
				return err
			}
			total += uint64(st.Size())
		}
		return nil
	})
	return total, err
}
func copyUpdateTree(ctx context.Context, src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := nativeinstall.SafePath(path); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		file, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(file, in)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

// Process exit and antivirus file-handle release can lag the service stop.
// Retry only transient Windows sharing/access failures; never delete a target.
func renameUpdateDirectory(from, to string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := os.Rename(from, to)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func updateInstallation(ctx context.Context, target string, report reporter) (resultErr error) {
	root, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	unlock, err := nativeinstall.Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = updatePreflight(root); err != nil {
		return err
	}
	size, err := treeSize(root)
	if err != nil {
		return err
	}
	_, unpacked, err := archiveInfo()
	if err != nil {
		return err
	}
	path, _ := windows.UTF16PtrFromString(filepath.Dir(root))
	var free uint64
	if err = windows.GetDiskFreeSpaceEx(path, &free, nil, nil); err != nil {
		return err
	}
	if free < size+unpacked+(256<<20) {
		return errors.New("磁盘空间不足：需要新程序、完整数据副本及 256 MiB 余量")
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	r := updateRecord{Root: root, Stage: root + ".update-" + stamp, Backup: root + ".backup-" + stamp, Failed: root + ".failed-" + stamp, Phase: "extracting"}
	if err = saveUpdate(r); err != nil {
		return err
	}
	// All failure paths retain staged/failed files. Recovery uses the durable
	// record and whole-directory snapshots, including the pre-migration DB.
	defer func() {
		if resultErr != nil {
			if r.Phase == "complete" {
				resultErr = fmt.Errorf("更新已经通过检查，但事务记录清理失败；重新运行安装器即可清理：%w", resultErr)
				return
			}
			report(installState{message: "更新未完成，正在恢复原安装", details: resultErr.Error()})
			if e := restoreUpdate(r, report); e != nil {
				resultErr = fmt.Errorf("%w；自动恢复未完成：%v；请重新运行安装器恢复，记录：%s", resultErr, e, updateJournal(root))
			} else {
				resultErr = fmt.Errorf("%w；已恢复原安装", resultErr)
			}
		}
	}()
	if err = install(ctx, r.Stage, true, false, report); err != nil {
		return err
	}
	r.Phase = "stopping"
	if err = saveUpdate(r); err != nil {
		return err
	}
	report(installState{message: "正在停止本套服务并备份数据库、App 和配置", position: 480, details: "完整备份将保留在：" + r.Backup})
	if err = nativeinstall.Stop(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".installing" || entry.Name() == "stop.request" {
			continue
		}
		// Package-owned top-level directories are replaced atomically; all
		// runtime state and unowned user files are copied without overwriting.
		if _, e := os.Stat(filepath.Join(r.Stage, entry.Name())); e == nil {
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		if err = copyUpdateTree(ctx, filepath.Join(root, entry.Name()), filepath.Join(r.Stage, entry.Name())); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(r.Stage, ".installing"), []byte("Update awaiting activation"), 0600); err != nil {
		return err
	}
	r.Phase = "switching"
	if err = saveUpdate(r); err != nil {
		return err
	}
	if err = renameUpdateDirectory(root, r.Backup); err != nil {
		return err
	}
	if err = renameUpdateDirectory(r.Stage, root); err != nil {
		return err
	}
	r.Phase = "verifying"
	if err = saveUpdate(r); err != nil {
		return err
	}
	if err = nativeinstall.RemoveMarker(root); err != nil {
		return err
	}
	if err = startInstalled(ctx, root, report); err != nil {
		return err
	}
	if err = nativeinstall.Register(root); err != nil {
		return err
	}
	complete := r
	complete.Phase = "complete"
	if err = saveUpdate(complete); err != nil {
		return err
	}
	r = complete
	if err = os.Remove(updateJournal(root)); err != nil {
		return err
	}
	report(installState{phase: "done", message: "更新完成，数据和节点身份已保留", position: 1000, details: "升级前完整备份：" + r.Backup})
	return nil
}
func restoreUpdate(r updateRecord, report reporter) error {
	if r.Phase == "extracting" {
		return os.Remove(updateJournal(r.Root))
	}
	_, backupErr := os.Stat(r.Backup)
	if backupErr == nil {
		r.Phase = "restoring"
		if err := saveUpdate(r); err != nil {
			return err
		}
		if _, err := os.Stat(r.Root); err == nil {
			if err = nativeinstall.Stop(r.Root); err != nil {
				return err
			}
			if err = renameUpdateDirectory(r.Root, r.Failed); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := renameUpdateDirectory(r.Backup, r.Root); err != nil {
			return err
		}
	} else if !os.IsNotExist(backupErr) {
		return backupErr
	}
	// Also handles a retry after restoring the directory but before restarting.
	// Never start a second supervisor beside one left by an interrupted recovery.
	if err := nativeinstall.Stop(r.Root); err != nil {
		return err
	}
	if err := nativeinstall.RemoveMarker(r.Root); err != nil {
		return err
	}
	if err := startInstalled(context.Background(), r.Root, report); err != nil {
		return err
	}
	if err := nativeinstall.RestoreRegistration(r.Root); err != nil {
		return err
	}
	return os.Remove(updateJournal(r.Root))
}
func recoverUpdate(target string, report reporter) error {
	root, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err = nativeinstall.SafePath(root); err != nil {
		return err
	}
	unlock, err := nativeinstall.Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	raw, err := os.ReadFile(updateJournal(root))
	if err != nil {
		return err
	}
	var r updateRecord
	if err = json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if r.Root != root {
		return errors.New("恢复记录的安装目录不匹配")
	}
	switch r.Phase {
	case "extracting", "stopping", "switching", "verifying", "restoring", "complete":
	default:
		return errors.New("无法识别更新阶段，保留所有目录以供诊断")
	}
	for path, suffix := range map[string]string{r.Stage: ".update-", r.Backup: ".backup-", r.Failed: ".failed-"} {
		if filepath.Dir(path) != filepath.Dir(root) || !strings.HasPrefix(path, root+suffix) {
			return errors.New("恢复记录路径不安全")
		}
		if err = nativeinstall.SafePath(path); err != nil {
			return err
		}
	}
	if r.Phase == "complete" {
		return os.Remove(updateJournal(root))
	}
	return restoreUpdate(r, report)
}

func checkDataCompatibility(root string, old, next nativeManifest) error {
	if old.Distribution.DataCompatibility.Format != 0 && old.Distribution.DataCompatibility != next.Distribution.DataCompatibility {
		return errors.New("incompatible package data contract")
	}
	if next.CreatedAt.Before(old.CreatedAt) {
		return errors.New("目标构建早于当前安装，禁止直接降级")
	}
	if old.PackageVersion != "" && old.PackageVersion == next.PackageVersion && old.Revision != next.Revision {
		return errors.New("同一安装包版本对应不同源码，拒绝更新")
	}
	if old.ProductVersion != "" {
		comparison, e := runtimeenv.CompareProductVersions(runtimeenv.NormalizeUnpublishedVersion(old.ProductVersion, old.Revision), next.ProductVersion)
		if e != nil || comparison > 0 {
			return errors.New("业务版本降级不受支持，请通过兼容备份恢复")
		}
	}
	pgMajor := func(m nativeManifest) string {
		for _, d := range m.Dependencies {
			if d.Name == "postgresql.zip" {
				return strings.Split(d.Version, ".")[0]
			}
		}
		return ""
	}
	if pgMajor(old) == "" || pgMajor(old) != pgMajor(next) {
		return errors.New("PostgreSQL 主版本不同，需要专门迁移")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "data", "postgres", "PG_VERSION")); err == nil && strings.TrimSpace(string(raw)) != pgMajor(next) {
		return errors.New("现有数据库主版本与安装包不匹配")
	}
	return nil
}

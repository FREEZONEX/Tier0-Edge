//go:build darwin || linux

package main

import (
	"backend/internal/runtimeenv"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func verifyProgram(root string, raw []byte) error {
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return errors.New("package file checksums missing")
	}
	for name, expected := range manifest.Files {
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("unsafe package file path")
		}
		file := filepath.Join(root, clean)
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package file not regular: %s", name)
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, f)
		f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != expected {
			return fmt.Errorf("package integrity failure: %s", name)
		}
	}
	return nil
}

type nativeRelease struct {
	Distribution   runtimeenv.Distribution `json:"distribution"`
	PackageVersion string                  `json:"packageVersion"`
	ProductVersion string                  `json:"productVersion"`
	SourceRevision string                  `json:"sourceRevision"`
	PostgresMajor  string                  `json:"postgresMajor"`
	DataFormat     int                     `json:"dataFormat"`
	UpgradeFrom    []string                `json:"supportedUpgradeFrom"`
	CreatedAt      time.Time               `json:"createdAt"`
}

func productVersion(programRoot string) string {
	raw, err := os.ReadFile(filepath.Join(programRoot, "package.json"))
	if err != nil {
		return "unknown"
	}
	var r nativeRelease
	if json.Unmarshal(raw, &r) != nil {
		return "unknown"
	}
	return r.ProductVersion
}

type nativeInstallState struct {
	nativeRelease
	CompletedAt string `json:"completedAt"`
	Backup      string `json:"backup,omitempty"`
}

func directoryBytes(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			total += uint64(i.Size())
		}
		return nil
	})
	return total, err
}

// Offline snapshot: preserve permissions and internal symlinks, never follow links outside data.
func copyDataTree(source, target, allowedRoot string) error {
	return filepath.WalkDir(source, func(p string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, p)
		if e != nil {
			return e
		}
		to := filepath.Join(target, rel)
		info, e := d.Info()
		if e != nil {
			return e
		}
		if d.IsDir() {
			return os.MkdirAll(to, info.Mode().Perm())
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, e := os.Readlink(p)
			if e != nil {
				return e
			}
			resolved, e := filepath.EvalSymlinks(p)
			if e != nil {
				return e
			}
			inside, e := filepath.Rel(allowedRoot, resolved)
			if e != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("backup refuses external link %s", p)
			}
			if filepath.IsAbs(link) {
				link, e = filepath.Rel(filepath.Dir(p), resolved)
				if e != nil {
					return e
				}
			}
			return os.Symlink(link, to)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup refuses special file %s", p)
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
}
func prepareInstallation(root, programRoot string) (func() error, func(error), error) {
	noop := func(error) {}
	fail := func(e error) (func() error, func(error), error) { return nil, noop, e }
	root = filepath.Clean(root)
	if root == programRoot || strings.HasPrefix(root, programRoot+string(os.PathSeparator)) || strings.Contains(root, ".app/") {
		return fail(errors.New("data directory must be outside the application bundle"))
	}
	raw, e := os.ReadFile(filepath.Join(programRoot, "package.json"))
	if e != nil {
		return fail(e)
	}
	var release nativeRelease
	if e = json.Unmarshal(raw, &release); e != nil {
		return fail(e)
	}
	if release.DataFormat != 1 || release.PostgresMajor != "17" || release.ProductVersion != runtimeenv.BuildVersion || !runtimeenv.ValidPackageVersion(release.ProductVersion, release.PackageVersion) || release.CreatedAt.IsZero() {
		return fail(errors.New("unsupported native package compatibility contract"))
	}
	if e = verifyProgram(programRoot, raw); e != nil {
		return fail(e)
	}
	stateFile := filepath.Join(root, "state", "install-state.json")
	pending := filepath.Join(root, "state", "upgrade-pending.json")
	if _, e = os.Stat(pending); e == nil {
		return fail(fmt.Errorf("previous initialization/upgrade did not complete; inspect %s and restore its backup to a new data directory before retrying", pending))
	} else if !os.IsNotExist(e) {
		return fail(e)
	}
	for _, original := range []string{"18080", "15432", "14222", "11883", "18083", "19732", "19443", "19733"} {
		p := servicePort(original)
		host := "127.0.0.1"
		if original == "18080" {
			host = nativeHTTPListenHost
		}
		l, e := net.Listen("tcp", net.JoinHostPort(host, p))
		if e != nil {
			return fail(fmt.Errorf("service port %s occupied; stop the running instance before initialization or migration", p))
		}
		l.Close()
	}
	if _, e = os.Stat(filepath.Join(root, "data", "postgres", "postmaster.pid")); e == nil {
		return fail(errors.New("PostgreSQL has a postmaster.pid; stop the database cleanly and inspect stale PID evidence before upgrade"))
	} else if !os.IsNotExist(e) {
		return fail(e)
	}
	var old nativeInstallState
	raw, e = os.ReadFile(stateFile)
	fresh := os.IsNotExist(e)
	if !fresh {
		if e != nil {
			return fail(e)
		}
		if e = json.Unmarshal(raw, &old); e != nil {
			return fail(e)
		}
		if old.Distribution.Edition != "" && old.Distribution.Identity != release.Distribution.Identity {
			return fail(errors.New("product conversion requires an explicit migration"))
		}
		// The only pre-unified macOS packages were immutable Edge ARM64 native builds.
		if old.Distribution.Edition == "" && (runtime.GOOS != "darwin" || runtimeenv.Edition() != "edge" || (old.PackageVersion != "0.1.0" && old.PackageVersion != "0.1.1" && old.PackageVersion != "0.1.2")) {
			return fail(errors.New("legacy product identity cannot be established"))
		}
		if old.DataFormat != release.DataFormat || old.PostgresMajor != release.PostgresMajor {
			return fail(errors.New("incompatible data format or PostgreSQL major; explicit migration and backup restore required"))
		}
		pg, e := os.ReadFile(filepath.Join(root, "data", "postgres", "PG_VERSION"))
		if e != nil {
			return fail(e)
		}
		if strings.TrimSpace(string(pg)) != release.PostgresMajor {
			return fail(errors.New("PG_VERSION does not match bundled PostgreSQL; refusing to start old data"))
		}
		if old.PackageVersion == release.PackageVersion {
			if old.SourceRevision != release.SourceRevision {
				return fail(errors.New("same package version has a different source revision; use a new supported package version"))
			}
			return func() error { return nil }, noop, nil
		}
		oldVersion := runtimeenv.NormalizeUnpublishedVersion(old.ProductVersion, old.SourceRevision)
		if oldVersion == "" {
			oldVersion = old.PackageVersion
		}
		comparison, versionErr := runtimeenv.CompareProductVersions(oldVersion, release.ProductVersion)
		if versionErr != nil || comparison > 0 || (!old.CreatedAt.IsZero() && release.CreatedAt.Before(old.CreatedAt)) {
			return fail(errors.New("business version downgrade or older build refused; restore a compatible backup"))
		}
		supported := oldVersion == release.ProductVersion || (strings.Split(oldVersion, "-r")[0] == "3.0.0" && strings.Split(release.ProductVersion, "-r")[0] == "3.0.0")
		for _, v := range release.UpgradeFrom {
			if oldVersion == v {
				supported = true
			}
		}
		if !supported {
			return fail(fmt.Errorf("upgrade %s -> %s unsupported; downgrade requires backup restore", old.PackageVersion, release.PackageVersion))
		}
	} else {
		entries, e := os.ReadDir(root)
		if e != nil {
			return fail(e)
		}
		for _, entry := range entries {
			if entry.Name() != "state" && entry.Name() != "logs" {
				return fail(errors.New("unrecognized nonempty data directory; refusing adoption or overwrite"))
			}
		}
	}
	var size uint64
	for _, name := range []string{"data", "state", "backend"} {
		n, e := directoryBytes(filepath.Join(root, name))
		if e != nil {
			return fail(e)
		}
		size += n
	}
	var disk syscall.Statfs_t
	if e = syscall.Statfs(root, &disk); e != nil {
		return fail(e)
	}
	need := uint64(512 << 20)
	if !fresh {
		need += size
	}
	if disk.Bavail*uint64(disk.Bsize) < need {
		return fail(fmt.Errorf("insufficient free space; need at least %d bytes for backup and migration", need))
	}
	backup := ""
	if !fresh {
		backup = filepath.Join(root, "backups", time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+old.PackageVersion)
		if e = os.MkdirAll(backup, 0700); e != nil {
			return fail(e)
		}
		for _, name := range []string{"data", "state", "backend"} {
			if e = copyDataTree(filepath.Join(root, name), filepath.Join(backup, name), root); e != nil {
				return fail(fmt.Errorf("backup incomplete at %s: %w", backup, e))
			}
		}
		if e = writeJSONAtomic(filepath.Join(backup, "backup.json"), map[string]any{"complete": true, "from": old, "targetVersion": release.PackageVersion, "bytes": size, "method": "offline physical copy"}); e != nil {
			return fail(e)
		}
	}
	attempt := map[string]any{"fromVersion": old.PackageVersion, "targetVersion": release.PackageVersion, "backup": backup, "startedAt": time.Now().UTC().Format(time.RFC3339), "status": "initializing"}
	if e = writeJSONAtomic(pending, attempt); e != nil {
		return fail(e)
	}
	completed := false
	return func() error {
			state := nativeInstallState{nativeRelease: release, CompletedAt: time.Now().UTC().Format(time.RFC3339), Backup: backup}
			if e := writeJSONAtomic(stateFile, state); e != nil {
				return e
			}
			attempt["status"] = "succeeded"
			if e := writeJSONAtomic(filepath.Join(root, "state", "last-upgrade.json"), attempt); e != nil {
				return e
			}
			if e := os.Remove(pending); e != nil {
				return e
			}
			completed = true
			return nil
		}, func(err error) {
			if completed {
				return
			}
			attempt["status"] = "failed"
			attempt["error"] = err.Error()
			_ = writeJSONAtomic(pending, attempt)
		}, nil
}

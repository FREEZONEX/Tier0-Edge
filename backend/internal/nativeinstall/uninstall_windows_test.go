package nativeinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallPreservesDataAndUnownedFiles(t *testing.T) {
	root := t.TempDir()
	managed := []string{"Start.exe", `backend\backend.exe`, `web\assets\main.js`}
	retained := []string{`data\postgres\rows`, `state\environment.json`, `logs\backend.log`, `backups\snapshot`, `web\my-notes.txt`, "customer.txt", "package.json", "Uninstall.exe"}
	for _, name := range append(managed, retained...) {
		fixtureFile(t, root, name, "original")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := removeProgramFiles(root, managed); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range managed {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("program remains: %s", name)
		}
	}
	for _, name := range retained {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(raw) != "original" {
			t.Fatalf("retained file changed: %s (%v)", name, err)
		}
	}
}

func TestUninstallInventoryRejectsUnsafePathsBeforeDeletion(t *testing.T) {
	base := "1  Start.exe\n1  backend/backend.exe\n1  Uninstall.exe\n1  package.json\n"
	for _, name := range []string{"../outside.txt", `backend\..\..\outside.txt`, "C:/victim", "data/postgres/rows", "state/environment.json", "backend/file:stream", "web/../state/key", "Start.exe\n1  START.EXE"} {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, "Start.exe", "keep")
			fixtureFile(t, root, "FILES.txt", base+"1  "+name+"\n")
			if _, err := uninstallFiles(root); err == nil {
				t.Fatal("unsafe inventory accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "Start.exe")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUninstallRejectsSymlinkAndDirectoryReplacement(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	fixtureFile(t, outside, "keep", "value")
	if err := os.Mkdir(filepath.Join(root, "Start.exe"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := removeProgramFiles(root, []string{"Start.exe"}); err == nil {
		t.Fatal("directory accepted as file")
	}
	if err := os.Symlink(outside, filepath.Join(root, "web")); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if err := SafePath(filepath.Join(root, "web", "keep")); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallMutationLockIsExclusiveAndScoped(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if second, err := Lock(root); err == nil {
		second()
		t.Fatal("concurrent mutation allowed")
	}
	other, err := Lock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other()
}

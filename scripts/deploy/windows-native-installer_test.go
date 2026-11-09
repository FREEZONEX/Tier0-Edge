//go:build ignore

package main

import (
	"archive/zip"
	"backend/internal/runtimeenv"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reinstallFixture(t *testing.T) (string, nativeManifest, *zip.Reader) {
	t.Helper()
	root := t.TempDir()
	m := nativeManifest{Kind: "windows-native-validation", Platform: "windows", Architecture: "amd64", Revision: runtimeenv.BuildRevision, ProductVersion: runtimeenv.BuildVersion, PackageVersion: runtimeenv.BuildVersion + "-aabbcc", CreatedAt: time.Now().UTC()}
	m.Distribution.Identity = runtimeenv.Identity()
	m.Distribution.ProductVersion = m.ProductVersion
	m.Distribution.CodeVersion = m.Revision
	m.Distribution.DataCompatibility.Format = 1
	m.Distribution.DataCompatibility.PostgresMajor = "17"
	m.Distribution.DataCompatibility.Metadata = "metadata/1"
	m.Distribution.DataCompatibility.Data = "data/3"
	m.Dependencies = append(m.Dependencies, struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}{"postgresql.zip", "17.11"})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "package.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for name, data := range map[string][]byte{"package.json": raw, "FILES.txt": []byte("inventory"), "Start.exe": []byte("program")} {
		f, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return root, m, r
}

func TestReinstallPreflightRetainsFilesAndRejectsUnsafeChanges(t *testing.T) {
	root, old, archive := reinstallFixture(t)
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "rows"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reinstallPreflight(root, archive); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Start.exe"), []byte("user file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reinstallPreflight(root, archive); err == nil {
		t.Fatal("overwriting collision accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Start.exe")); string(data) != "user file" {
		t.Fatal("collision was mutated")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "data", "rows")); string(data) != "keep" {
		t.Fatal("data was mutated")
	}
	other, _, next := reinstallFixture(t)
	old.Revision = "different-source"
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(other, "package.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := reinstallPreflight(other, next); err == nil {
		t.Fatal("cross-revision reinstall bypassed backup upgrade")
	}
}

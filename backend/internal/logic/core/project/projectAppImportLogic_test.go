package project

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
)

func TestParsePackageFromPath_ReadsRolesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"demo-app\"\n"))
	writeZipFile(t, zw, "roles.json", []byte(`[
		{"roleKey":"operator","name":"操作员","description":"日常操作"},
		{"roleKey":"viewer","name":"观众"}
	]`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, cleanup, err := ParsePackageFromPath(zipPath)
	if err != nil {
		t.Fatalf("ParsePackageFromPath failed: %v", err)
	}
	defer cleanup()

	if pkg.AppName != "demo-app" {
		t.Errorf("AppName = %q, want demo-app", pkg.AppName)
	}
	if len(pkg.RolesRaw) == 0 {
		t.Fatal("expected RolesRaw to be populated")
	}

	roles, err := parseProjectAppRoles(pkg.RolesRaw)
	if err != nil {
		t.Fatalf("parseProjectAppRoles failed: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles))
	}
	if roles[0].Role != "operator" || roles[0].DisplayName != "操作员" {
		t.Errorf("first role = %+v, want operator/操作员", roles[0])
	}
	if roles[1].Role != "viewer" || roles[1].DisplayName != "观众" {
		t.Errorf("second role = %+v, want viewer/观众", roles[1])
	}
}

func TestParsePackageFromPath_RolesJSONOptional(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"no-roles-app\"\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, cleanup, err := ParsePackageFromPath(zipPath)
	if err != nil {
		t.Fatalf("ParsePackageFromPath failed: %v", err)
	}
	defer cleanup()

	if pkg.RolesRaw != nil {
		t.Errorf("expected RolesRaw nil when roles.json absent, got %q", string(pkg.RolesRaw))
	}
}

func TestParsePackageFromPath_ZIP64(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "zip64-app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"zip64-app\"\n"))
	for i := 0; i < 65_535; i++ {
		writeZipFile(t, zw, filepath.Join("empty", fmt.Sprintf("%05d", i)), nil)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, cleanup, err := ParsePackageFromPath(zipPath)
	if err != nil {
		t.Fatalf("ParsePackageFromPath failed for ZIP64 archive: %v", err)
	}
	defer cleanup()
	if pkg.AppName != "zip64-app" {
		t.Errorf("AppName = %q, want zip64-app", pkg.AppName)
	}
}

func TestParsePackageFromPath_RejectsUnsafePath(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "unsafe-app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"unsafe-app\"\n"))
	writeZipFile(t, zw, "../escape.txt", []byte("escaped"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	_, cleanup, err := ParsePackageFromPath(zipPath)
	cleanup()
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("error = %v, want ErrUnsupportedPackageFormat", err)
	}
	if err == nil || !strings.Contains(err.Error(), "unsafe zip entry path") {
		t.Fatalf("error = %v, want unsafe path detail", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "escape.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("escape path stat error = %v, want not exists", statErr)
	}
}

func TestParseProjectAppMeta_ExtendedFields(t *testing.T) {
	raw := []byte(`{
		"name": "demo-app",
		"displayName": "Demo App",
		"description": "演示应用",
		"version": 3,
		"workspaceId": "ws-1",
		"projectId": "prj-2",
		"appId": "app-3",
		"schemaAlias": "demo_schema",
		"changelog": "修复若干问题"
	}`)
	meta, err := parseProjectAppMeta(raw)
	if err != nil {
		t.Fatalf("parseProjectAppMeta failed: %v", err)
	}
	if meta.Name != "demo-app" || meta.DisplayName != "Demo App" || meta.Description != "演示应用" {
		t.Errorf("unexpected basic fields: %+v", meta)
	}
	if meta.Version != "3" {
		t.Errorf("Version = %q, want 3（JSON 整数 version code 应转为字符串）", meta.Version)
	}
	if meta.WorkspaceID != "ws-1" || meta.ProjectID != "prj-2" || meta.AppID != "app-3" {
		t.Errorf("unexpected identity fields: %+v", meta)
	}
	if meta.SchemaAlias != "demo_schema" || meta.Changelog != "修复若干问题" {
		t.Errorf("unexpected schemaAlias/changelog: %+v", meta)
	}
}

func TestParseProjectAppMeta_WrappedApp(t *testing.T) {
	// {"app":{...}} 包裹形式同样解析新字段
	raw := []byte(`{"app":{"name":"wrapped","workspaceId":"ws","projectId":"prj","appId":"app","version":7}}`)
	meta, err := parseProjectAppMeta(raw)
	if err != nil {
		t.Fatalf("parseProjectAppMeta failed: %v", err)
	}
	if meta.Name != "wrapped" || meta.WorkspaceID != "ws" || meta.ProjectID != "prj" || meta.AppID != "app" {
		t.Errorf("unexpected meta: %+v", meta)
	}
	if meta.Version != "7" {
		t.Errorf("Version = %q, want 7", meta.Version)
	}
}

func TestValidateReplaceIdentity(t *testing.T) {
	fullManifest := map[string]any{
		"sourceWorkspaceId": "ws-1",
		"sourceProjectId":   "prj-1",
		"sourceAppId":       "app-1",
	}
	fullMeta := projectAppMeta{WorkspaceID: "ws-1", ProjectID: "prj-1", AppID: "app-1"}

	cases := []struct {
		name     string
		manifest map[string]any
		meta     projectAppMeta
		wantErr  error
	}{
		{
			name:     "三 ID 全一致，允许覆盖导入",
			manifest: fullManifest,
			meta:     fullMeta,
			wantErr:  nil,
		},
		{
			name:     "appId 一致但 projectId 不一致，仅记录不校验，允许覆盖导入",
			manifest: fullManifest,
			meta:     projectAppMeta{WorkspaceID: "ws-1", ProjectID: "prj-other", AppID: "app-1"},
			wantErr:  nil,
		},
		{
			name:     "appId 一致但 workspaceId 不一致，仅记录不校验，允许覆盖导入",
			manifest: fullManifest,
			meta:     projectAppMeta{WorkspaceID: "ws-other", ProjectID: "prj-1", AppID: "app-1"},
			wantErr:  nil,
		},
		{
			name:     "appId 不一致，拒绝",
			manifest: fullManifest,
			meta:     projectAppMeta{WorkspaceID: "ws-1", ProjectID: "prj-1", AppID: "app-other"},
			wantErr:  domainproject.ErrAppIdentityNotMatch,
		},
		{
			name:     "目标应用未记录来源身份，无法校验",
			manifest: map[string]any{},
			meta:     fullMeta,
			wantErr:  domainproject.ErrAppIdentityUnverifiable,
		},
		{
			name: "目标应用仅记录 sourceAppId 即可校验（projectId/workspaceId 不参与校验）",
			manifest: map[string]any{
				"sourceAppId": "app-1",
			},
			meta:    fullMeta,
			wantErr: nil,
		},
		{
			name:     "包内 meta.json 缺 appId，无法校验",
			manifest: fullManifest,
			meta:     projectAppMeta{WorkspaceID: "ws-1", ProjectID: "prj-1"},
			wantErr:  domainproject.ErrAppIdentityUnverifiable,
		},
		{
			name:     "包内无 meta.json（三 ID 全空），无法校验",
			manifest: fullManifest,
			meta:     projectAppMeta{},
			wantErr:  domainproject.ErrAppIdentityUnverifiable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReplaceIdentity(tc.manifest, tc.meta)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateReplaceIdentity() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateReplaceIdentity() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestReadProjectAppPackageFromPath_FlowJSONPriority(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "meta.json", []byte(`{"name":"flow-app"}`))
	writeZipFile(t, zw, "flow.json", []byte(`[{"id":"new-flow","type":"tab","label":"new"}]`))
	writeZipFile(t, zw, "flows.json", []byte(`[{"id":"legacy-flow","type":"tab","label":"legacy"}]`))
	writeZipFile(t, zw, "icon.png", []byte{0x89, 0x50, 0x4e, 0x47})
	writeZipFile(t, zw, "cover.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	// flow.json 优先于 flows.json
	if !strings.Contains(string(pkg.FlowRaw), "new-flow") {
		t.Errorf("FlowRaw = %s, want flow.json 内容优先", string(pkg.FlowRaw))
	}
	// icon/cover 解析
	if len(pkg.IconRaw) == 0 || pkg.IconName != "icon.png" {
		t.Errorf("Icon = (%q, %d bytes), want icon.png 内容", pkg.IconName, len(pkg.IconRaw))
	}
	if len(pkg.CoverRaw) == 0 || pkg.CoverName != "cover.svg" {
		t.Errorf("Cover = (%q, %d bytes), want cover.svg 内容", pkg.CoverName, len(pkg.CoverRaw))
	}
}

func TestReadProjectAppPackageFromPath_PublicAssetsTakePriorityAndOtherNestedFilesAreIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "meta.json", []byte(`{"name":"asset-app"}`))
	writeZipFile(t, zw, "icon.png", []byte("root-icon"))
	writeZipFile(t, zw, "cover.png", []byte("root-cover"))
	writeZipFile(t, zw, "public/app-icon.svg", []byte("public-icon"))
	writeZipFile(t, zw, "public/app-cover.webp", []byte("public-cover"))
	writeZipFile(t, zw, "assets/icon.png", []byte("nested-icon"))
	writeZipFile(t, zw, "docs/cover.webp", []byte("nested-cover"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if string(pkg.IconRaw) != "public-icon" || pkg.IconName != "app-icon.svg" {
		t.Errorf("Icon = (%q, %q), want public/app-icon.svg 内容优先", pkg.IconName, string(pkg.IconRaw))
	}
	if string(pkg.CoverRaw) != "public-cover" || pkg.CoverName != "app-cover.webp" {
		t.Errorf("Cover = (%q, %q), want public/app-cover.webp 内容优先", pkg.CoverName, string(pkg.CoverRaw))
	}
}

func TestReadProjectAppPackageFromPath_IgnoresAssetsOutsidePublic(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "meta.json", []byte(`{"name":"asset-app"}`))
	writeZipFile(t, zw, "assets/app-icon.png", []byte("nested-icon"))
	writeZipFile(t, zw, "docs/app-cover.webp", []byte("nested-cover"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if len(pkg.IconRaw) != 0 || pkg.IconName != "" {
		t.Errorf("Icon = (%q, %d bytes), want non-public nested icon ignored", pkg.IconName, len(pkg.IconRaw))
	}
	if len(pkg.CoverRaw) != 0 || pkg.CoverName != "" {
		t.Errorf("Cover = (%q, %d bytes), want non-public nested cover ignored", pkg.CoverName, len(pkg.CoverRaw))
	}
}

func TestReadProjectAppPackageFromPath_EmptyPublicAssetFallsBackToRoot(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "app/meta.json", []byte(`{"name":"asset-app"}`))
	writeZipFile(t, zw, "app/public/app-icon.png", nil)
	writeZipFile(t, zw, "app/icon.svg", []byte("root-icon"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if string(pkg.IconRaw) != "root-icon" || pkg.IconName != "icon.svg" {
		t.Errorf("Icon = (%q, %q), want empty public candidate skipped and app/icon.svg used", pkg.IconName, string(pkg.IconRaw))
	}
}

func TestReadProjectAppPackageFromPath_RootMediaAliases(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "app/meta.json", []byte(`{"name":"asset-app"}`))
	writeZipFile(t, zw, "app/app_icon.svg", []byte("root-icon"))
	writeZipFile(t, zw, "app/app-cover-image.webp", []byte("root-cover"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if string(pkg.IconRaw) != "root-icon" || pkg.IconName != "app_icon.svg" {
		t.Errorf("Icon = (%q, %q), want root app_icon.svg alias", pkg.IconName, string(pkg.IconRaw))
	}
	if string(pkg.CoverRaw) != "root-cover" || pkg.CoverName != "app-cover-image.webp" {
		t.Errorf("Cover = (%q, %q), want root app-cover-image.webp alias", pkg.CoverName, string(pkg.CoverRaw))
	}
}

func TestReadProjectAppPackageFromPath_FlowsJSONFallback(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "meta.json", []byte(`{"name":"flow-app"}`))
	writeZipFile(t, zw, "flows.json", []byte(`[{"id":"legacy-flow","type":"tab","label":"legacy"}]`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	// 无 flow.json 时兼容旧版 flows.json
	if !strings.Contains(string(pkg.FlowRaw), "legacy-flow") {
		t.Errorf("FlowRaw = %s, want flows.json 内容兜底", string(pkg.FlowRaw))
	}
}

func TestParsePackageFromPath_ArtifactExtraFiles(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"demo-app\"\n"))
	writeZipFile(t, zw, "meta.json", []byte(`{"workspaceId":"ws-1","projectId":"prj-1","appId":"app-1","version":2,"changelog":"v2"}`))
	writeZipFile(t, zw, "uns.json", []byte(`{"namespace":[{"name":"ns1","type":"FOLDER"}]}`))
	writeZipFile(t, zw, "flow.json", []byte(`[{"id":"f1","type":"tab","label":"flow"}]`))
	writeZipFile(t, zw, "icon.png", []byte("root-icon"))
	writeZipFile(t, zw, "public/app-icon.png", nil)
	writeZipFile(t, zw, "public/icon.svg", []byte("public-icon"))
	writeZipFile(t, zw, "public/cover.svg", []byte("public-cover"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, cleanup, err := ParsePackageFromPath(zipPath)
	if err != nil {
		t.Fatalf("ParsePackageFromPath failed: %v", err)
	}
	defer cleanup()

	// artifact 包根目录的附加文件应全部解析
	if pkg.Meta.AppID != "app-1" || pkg.Meta.ProjectID != "prj-1" || pkg.Meta.WorkspaceID != "ws-1" {
		t.Errorf("Meta identity = %+v", pkg.Meta)
	}
	if pkg.Meta.Version != "2" || pkg.Meta.Changelog != "v2" {
		t.Errorf("Meta version/changelog = %+v", pkg.Meta)
	}
	if len(pkg.UnsRaw) == 0 || !strings.Contains(string(pkg.UnsRaw), "ns1") {
		t.Errorf("UnsRaw = %q", string(pkg.UnsRaw))
	}
	if len(pkg.FlowRaw) == 0 || !strings.Contains(string(pkg.FlowRaw), "f1") {
		t.Errorf("FlowRaw = %q", string(pkg.FlowRaw))
	}
	if string(pkg.IconRaw) != "public-icon" || pkg.IconName != "icon.svg" {
		t.Errorf("Icon = (%q, %q), want empty public/app-icon.png skipped and public/icon.svg used", pkg.IconName, string(pkg.IconRaw))
	}
	if string(pkg.CoverRaw) != "public-cover" || pkg.CoverName != "cover.svg" {
		t.Errorf("Cover = (%q, %q), want public/cover.svg 内容", pkg.CoverName, string(pkg.CoverRaw))
	}
}

func TestParsePackageFromPath_ArtifactRootMediaAliases(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"demo-app\"\n"))
	writeZipFile(t, zw, "meta.json", []byte(`{"appId":"app-1"}`))
	writeZipFile(t, zw, "app-icon.svg", []byte("root-icon"))
	writeZipFile(t, zw, "app_cover.webp", []byte("root-cover"))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, cleanup, err := ParsePackageFromPath(zipPath)
	if err != nil {
		t.Fatalf("ParsePackageFromPath failed: %v", err)
	}
	defer cleanup()
	if string(pkg.IconRaw) != "root-icon" || pkg.IconName != "app-icon.svg" {
		t.Errorf("Icon = (%q, %q), want root app-icon.svg alias", pkg.IconName, string(pkg.IconRaw))
	}
	if string(pkg.CoverRaw) != "root-cover" || pkg.CoverName != "app_cover.webp" {
		t.Errorf("Cover = (%q, %q), want root app_cover.webp alias", pkg.CoverName, string(pkg.CoverRaw))
	}
}

func TestMatchPackageAssetFile(t *testing.T) {
	cases := []struct {
		base   string
		prefix string
		wantOK bool
	}{
		{"icon.png", "icon", true},
		{"icon.jpg", "icon", true},
		{"icon.jpeg", "icon", true},
		{"icon.svg", "icon", true},
		{"icon.webp", "icon", true},
		{"icon.gif", "icon", false},
		{"icon.png.bak", "icon", false},
		{"myicon.png", "icon", false},
		{"cover.png", "icon", false},
		{"icon", "icon", false},
	}
	for _, tc := range cases {
		if _, ok := matchPackageAssetFile(tc.base, tc.prefix); ok != tc.wantOK {
			t.Errorf("matchPackageAssetFile(%q, %q) = %v, want %v", tc.base, tc.prefix, ok, tc.wantOK)
		}
	}
}

func TestReadPackageMetaFromZip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"demo-app\"\n"))
	writeZipFile(t, zw, "meta.json", []byte(`{"workspaceId":"ws-1","projectId":"prj-1","appId":"app-1"}`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	meta, err := readPackageMetaFromZip(zipPath)
	if err != nil {
		t.Fatalf("readPackageMetaFromZip failed: %v", err)
	}
	if meta.AppID != "app-1" || meta.ProjectID != "prj-1" || meta.WorkspaceID != "ws-1" {
		t.Errorf("unexpected meta: %+v", meta)
	}

	// 无 meta.json 的包返回空 meta，不报错
	zipPath2 := filepath.Join(tmpDir, "app2.zip")
	zipFile2 := mustCreate(t, zipPath2)
	zw2 := zip.NewWriter(zipFile2)
	writeZipFile(t, zw2, "artifact.toml", []byte("name = \"demo-app\"\n"))
	if err := zw2.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile2.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	meta2, err := readPackageMetaFromZip(zipPath2)
	if err != nil {
		t.Fatalf("readPackageMetaFromZip without meta.json should not fail: %v", err)
	}
	if meta2 != (projectAppMeta{}) {
		t.Errorf("expected empty meta, got %+v", meta2)
	}
}

func mustCreate(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	return f
}

func writeZipFile(t *testing.T, zw *zip.Writer, name string, data []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

func TestReadPackageMetaFromZip_InvalidMetaReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "artifact.toml", []byte("name = \"demo-app\"\n"))
	writeZipFile(t, zw, "meta.json", []byte(`{"workspaceId":`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	// meta.json 存在但解析失败时必须返回错误（覆盖导入据此包装为 400 包格式错误）
	if _, err := readPackageMetaFromZip(zipPath); err == nil {
		t.Fatal("expected error for malformed meta.json, got nil")
	}
}

func TestBuildRestoredManifest(t *testing.T) {
	snapshot := json.RawMessage(`{"app_id":"docker-1","deploying":true,"unsNodeIds":[1,2],"roleIds":[3]}`)
	restored := buildRestoredManifest(snapshot, "deploy failed: boom")

	manifest := map[string]any{}
	if err := json.Unmarshal(restored, &manifest); err != nil {
		t.Fatalf("restored manifest is not valid JSON: %v", err)
	}
	if manifest["deploying"] != false {
		t.Errorf("expected deploying=false, got %v", manifest["deploying"])
	}
	if manifest["deploy_error"] != "deploy failed: boom" {
		t.Errorf("unexpected deploy_error: %v", manifest["deploy_error"])
	}
	// 快照中的既有键必须保留（新包写入的 unsNodeIds/roleIds 被快照覆盖还原）
	if manifest["app_id"] != "docker-1" {
		t.Errorf("expected app_id preserved from snapshot, got %v", manifest["app_id"])
	}
	ids, ok := manifest["unsNodeIds"].([]any)
	if !ok || len(ids) != 2 {
		t.Errorf("expected unsNodeIds restored from snapshot, got %v", manifest["unsNodeIds"])
	}
}

func TestBuildRestoredManifest_EmptySnapshot(t *testing.T) {
	restored := buildRestoredManifest(nil, "unpack failed: bad zip")
	manifest := map[string]any{}
	if err := json.Unmarshal(restored, &manifest); err != nil {
		t.Fatalf("restored manifest is not valid JSON: %v", err)
	}
	if manifest["deploying"] != false || manifest["deploy_error"] != "unpack failed: bad zip" {
		t.Errorf("unexpected manifest: %v", manifest)
	}
}

func TestRoleIDsFromBindings(t *testing.T) {
	bindings := []repo.ProjectRoleApp{
		{ProjectID: 1, RoleID: 7, AppID: 100},
		{ProjectID: 1, RoleID: 7, AppID: 101},
		{ProjectID: 1, RoleID: 9, AppID: 100},
	}
	got := roleIDsFromBindings(bindings)
	want := []int64{7, 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := roleIDsFromBindings(nil); len(got) != 0 {
		t.Fatalf("expected empty result for nil bindings, got %v", got)
	}
}

func TestBuildProjectAppPackage_CollectsAllFlowFiles(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "flow.json"), []byte(`[{"id":"flow"}]`), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "sourceflow.json"), []byte(`[{"id":"source"}]`), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "eventflow.json"), []byte(`[{"id":"event"}]`), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "flows.json"), []byte(`[{"id":"flows"}]`), 0o644)
	flowsDir := filepath.Join(tmpDir, "flows")
	_ = os.MkdirAll(flowsDir, 0o755)
	_ = os.WriteFile(filepath.Join(flowsDir, "a.json"), []byte(`[{"id":"a"}]`), 0o644)
	_ = os.WriteFile(filepath.Join(flowsDir, "event-b.json"), []byte(`[{"id":"event-b"}]`), 0o644)

	pkg := &Package{TempDir: tmpDir}
	appPkg, err := buildProjectAppPackage(pkg, nil)
	if err != nil {
		t.Fatalf("buildProjectAppPackage failed: %v", err)
	}
	if len(appPkg.FlowFiles) != 6 {
		t.Fatalf("expected 6 flow files, got %d: %+v", len(appPkg.FlowFiles), appPkg.FlowFiles)
	}
	flowTypes := map[string]int{}
	for _, f := range appPkg.FlowFiles {
		flowTypes[f.Name] = f.FlowType
	}
	if flowTypes["flow"] != 1 || flowTypes["sourceflow"] != 1 || flowTypes["flows"] != 1 || flowTypes["a"] != 1 {
		t.Errorf("unexpected source flow types: %+v", flowTypes)
	}
	if flowTypes["eventflow"] != 2 || flowTypes["event-b"] != 2 {
		t.Errorf("unexpected event flow types: %+v", flowTypes)
	}
}

func TestBuildProjectAppPackage_InvalidUnsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	pkg := &Package{
		TempDir: tmpDir,
		UnsRaw:  []byte(`{"namespace":`),
	}
	_, err := buildProjectAppPackage(pkg, nil)
	if err == nil {
		t.Fatal("expected error for invalid uns.json, got nil")
	}
}

// TestArtifactRedeployTargetFromApp 覆盖同名 artifact 包导入走更新路径时的目标校验与部署信息复用：
// 已部署动态应用应提取 docker app_id/host_port/access_mode/container_name（容器名复用机制），
// 部署中/非动态/缺 docker app_id 的目标都应明确报错而不是静默新建或重复部署。
func TestArtifactRedeployTargetFromApp(t *testing.T) {
	// 已部署的动态应用：提取可复用的部署信息（容器名/端口/路由身份）
	app := repo.ProjectApp{
		ID:       10,
		Name:     "my-app",
		SiteType: "dynamic",
		Status:   "active",
		ManifestJSON: json.RawMessage(`{
			"app_id": "docker-uuid-1",
			"host_port": 8123,
			"access_mode": "port",
			"container_name": "app-my-app-abc123"
		}`),
	}
	dockerAppID, hostPort, accessMode, containerName, err := artifactRedeployTargetFromApp(app, "port")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dockerAppID != "docker-uuid-1" || hostPort != 8123 || accessMode != "port" || containerName != "app-my-app-abc123" {
		t.Errorf("got (%q, %d, %q, %q), want 复用既有部署信息", dockerAppID, hostPort, accessMode, containerName)
	}

	// 目标正在部署中：拒绝并发更新（而不是新建或覆盖）
	app.Status = "deploying"
	if _, _, _, _, err := artifactRedeployTargetFromApp(app, "port"); !errors.Is(err, domainproject.ErrAppReplaceInProgress) {
		t.Fatalf("deploying app error = %v, want ErrAppReplaceInProgress", err)
	}

	// 部署失败的 App：与 static/manual 同等拒绝覆盖更新
	app.Status = "failed"
	if _, _, _, _, err := artifactRedeployTargetFromApp(app, "port"); !errors.Is(err, domainproject.ErrAppReplaceInvalidTarget) {
		t.Fatalf("failed app error = %v, want ErrAppReplaceInvalidTarget", err)
	}

	// 同名静态应用：没有可复用的容器/端口，明确报错
	app.Status = "active"
	app.SiteType = "sameOriginStatic"
	if _, _, _, _, err := artifactRedeployTargetFromApp(app, "port"); !errors.Is(err, domainproject.ErrAppReplaceInvalidTarget) {
		t.Fatalf("static app error = %v, want ErrAppReplaceInvalidTarget", err)
	}

	// 动态但未部署成功（manifest 缺 docker app_id）：同样无法原地重建
	app.SiteType = "dynamic"
	app.ManifestJSON = json.RawMessage(`{"host_port": 8123}`)
	if _, _, _, _, err := artifactRedeployTargetFromApp(app, "port"); !errors.Is(err, domainproject.ErrAppReplaceInvalidTarget) {
		t.Fatalf("missing app_id error = %v, want ErrAppReplaceInvalidTarget", err)
	}

	// 缺 container_name 时按 app.Name+dockerAppID 派生，保证更新后容器名与首次一致、路由不悬空
	app.ManifestJSON = json.RawMessage(`{"app_id": "docker-uuid-1", "host_port": 8123, "access_mode": "subdomain"}`)
	_, _, accessMode, containerName, err = artifactRedeployTargetFromApp(app, "port")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := appContainerName("my-app", "docker-uuid-1"); containerName != want {
		t.Errorf("containerName = %q, want %q（按 appName+dockerAppID 派生）", containerName, want)
	}
	if accessMode != "subdomain" {
		t.Errorf("accessMode = %q, want subdomain（manifest 记录优先）", accessMode)
	}

	// 未记录 access_mode 时使用默认 access mode
	app.ManifestJSON = json.RawMessage(`{"app_id": "docker-uuid-1", "host_port": 8123}`)
	_, _, accessMode, _, err = artifactRedeployTargetFromApp(app, "port")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accessMode != "port" {
		t.Errorf("accessMode = %q, want 默认 port", accessMode)
	}
}

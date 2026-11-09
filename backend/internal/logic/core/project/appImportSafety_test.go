package project

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainproject "backend/internal/domain/project"
)

// zipEntrySpec 构造测试 zip 的条目；mode 为零值时按普通文件处理。
type zipEntrySpec struct {
	name string
	data []byte
	mode fs.FileMode
}

// writeTestZip 生成含指定条目的 zip 文件并返回路径（普通文件/目录/symlink 均可）。
func writeTestZip(t *testing.T, fileName string, entries ...zipEntrySpec) string {
	t.Helper()
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, fileName)
	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create entry %q: %v", e.name, err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatalf("write entry %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return zipPath
}

// writeManyEntryZip 生成含 n 个空条目的 zip 文件（用于 entry 数上限校验）。
func writeManyEntryZip(t *testing.T, n int) string {
	t.Helper()
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "many.zip")
	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	for i := 0; i < n; i++ {
		if _, err := zw.Create(fmt.Sprintf("f/%06d", i)); err != nil {
			t.Fatalf("create entry %d: %v", i, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return zipPath
}

// 静态包统一 ZIP 安全校验：readProjectAppPackageFromPath 在逐 entry 读取前
// 复用 validateZipEntry 校验全部条目，恶意/畸形包一律 ErrUnsupportedPackageFormat，不创建 App。

func TestReadProjectAppPackageFromPath_RejectsPathTraversal(t *testing.T) {
	zipPath := writeTestZip(t, "traversal.zip",
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"ok"}`)},
		zipEntrySpec{name: "../escape.txt", data: []byte("x")},
	)
	_, err := readProjectAppPackageFromPath(zipPath, "traversal.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsAbsolutePath(t *testing.T) {
	zipPath := writeTestZip(t, "abs.zip",
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"ok"}`)},
		zipEntrySpec{name: "/etc/passwd", data: []byte("x")},
	)
	_, err := readProjectAppPackageFromPath(zipPath, "abs.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsBackslashTraversal(t *testing.T) {
	zipPath := writeTestZip(t, "backslash.zip",
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"ok"}`)},
		zipEntrySpec{name: `..\escape.txt`, data: []byte("x")},
	)
	_, err := readProjectAppPackageFromPath(zipPath, "backslash.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsSymlink(t *testing.T) {
	zipPath := writeTestZip(t, "symlink.zip",
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"ok"}`)},
		zipEntrySpec{name: "link", data: []byte("/etc/passwd"), mode: fs.ModeSymlink},
	)
	_, err := readProjectAppPackageFromPath(zipPath, "symlink.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

// TestReadProjectAppPackageFromPath_RejectsNonUnixCreatorSymlink 非 Unix creator（如 Python
// zipfile 默认 create_system=0）编码的 symlink entry：file.Mode() 不映射类型位可绕过
// 旧校验，必须按 raw external attrs 的 S_IFMT 拒绝（EIMP-009）。
func TestReadProjectAppPackageFromPath_RejectsNonUnixCreatorSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "symlink-dos.zip")
	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	// 普通 meta.json
	w, err := zw.Create("meta.json")
	if err != nil {
		t.Fatalf("create meta.json: %v", err)
	}
	if _, err := w.Write([]byte(`{"name":"ok"}`)); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
	// 非 Unix creator + external attrs 高 16 位标记 S_IFLNK|0777
	hdr := &zip.FileHeader{
		Name:           "link",
		Method:         zip.Deflate,
		CreatorVersion: 0, // creator = MS-DOS，Mode() 不会映射 S_IFMT
		ExternalAttrs:  0xA1FF << 16,
	}
	lw, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	if _, err := lw.Write([]byte("/etc/passwd")); err != nil {
		t.Fatalf("write link: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	_, err = readProjectAppPackageFromPath(zipPath, "symlink-dos.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat（非 Unix creator 的 symlink 必须拒绝）", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsEmptyZip(t *testing.T) {
	zipPath := writeTestZip(t, "empty.zip")
	_, err := readProjectAppPackageFromPath(zipPath, "empty.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsNoMetaJSON(t *testing.T) {
	zipPath := writeTestZip(t, "nometa.zip",
		zipEntrySpec{name: "icon.png", data: []byte("png")},
	)
	_, err := readProjectAppPackageFromPath(zipPath, "nometa.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

func TestReadProjectAppPackageFromPath_RejectsTooManyEntries(t *testing.T) {
	zipPath := writeManyEntryZip(t, maxPackageEntries+1)
	_, err := readProjectAppPackageFromPath(zipPath, "many.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

// 有 meta.json 但 name 为空时保留文件名回退（名称兜底链的一环），不拒绝。
func TestReadProjectAppPackageFromPath_MetaNameFallbackToFileName(t *testing.T) {
	zipPath := writeTestZip(t, "static-fallback-app.zip",
		zipEntrySpec{name: "meta.json", data: []byte(`{"workspaceId":"ws","projectId":"prj","appId":"app"}`)},
	)
	pkg, err := readProjectAppPackageFromPath(zipPath, "static-fallback-app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if pkg.Meta.Name != "static-fallback-app" {
		t.Errorf("Meta.Name = %q, want static-fallback-app", pkg.Meta.Name)
	}
}

// 兜底链层级：包内名称 → 文件名去扩展名 → app-时间戳（用户指定名由调用方优先应用）。
func TestResolveAppNameFallback(t *testing.T) {
	// 包内名称（artifact.toml/meta.json name）直接生效
	if got := resolveAppNameFallback("in-package", "/tmp/whatever.zip"); got != "in-package" {
		t.Errorf("packageName = %q, want in-package", got)
	}
	// 包内名称为空白时降级到文件名
	if got := resolveAppNameFallback("  ", "/tmp/ignored.zip"); got != "ignored" {
		t.Errorf("blank packageName: got %q, want ignored", got)
	}
	// 文件名去扩展名
	if got := resolveAppNameFallback("", "/tmp/exported-app.zip"); got != "exported-app" {
		t.Errorf("filename fallback: got %q, want exported-app", got)
	}
	if got := resolveAppNameFallback("", "/tmp/exported-app.ZIP"); got != "exported-app" {
		t.Errorf("filename fallback (uppercase ext): got %q, want exported-app", got)
	}
	// 全部缺失 → app-时间戳
	got := resolveAppNameFallback("", "")
	if !strings.HasPrefix(got, "app-") {
		t.Fatalf("timestamp fallback: got %q, want app- prefix", got)
	}
	if _, err := time.Parse("20060102150405", strings.TrimPrefix(got, "app-")); err != nil {
		t.Errorf("timestamp fallback: %q 后缀不是合法时间戳: %v", got, err)
	}
}

// artifact 快速读路径的名称兜底链：artifact.toml name 优先，其次包内 meta.json name，
// 再次包文件名（去扩展名），最后 app-时间戳（由 resolveAppNameFallback 覆盖）。
func TestReadArtifactFromZip_NameFallbackChain(t *testing.T) {
	// artifact.toml name 优先于 meta.json name
	zipPath := writeTestZip(t, "chain.zip",
		zipEntrySpec{name: "artifact.toml", data: []byte("name = \"toml-app\"\n")},
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"meta-app"}`)},
	)
	info, err := readArtifactFromZip(zipPath)
	if err != nil {
		t.Fatalf("readArtifactFromZip failed: %v", err)
	}
	if info.AppName != "toml-app" {
		t.Errorf("AppName = %q, want toml-app（artifact.toml name 优先）", info.AppName)
	}

	// artifact.toml 缺 name 时用包内 meta.json name
	zipPath2 := writeTestZip(t, "chain2.zip",
		zipEntrySpec{name: "artifact.toml", data: []byte("# no name\n")},
		zipEntrySpec{name: "meta.json", data: []byte(`{"name":"meta-app"}`)},
	)
	info2, err := readArtifactFromZip(zipPath2)
	if err != nil {
		t.Fatalf("readArtifactFromZip failed: %v", err)
	}
	if info2.AppName != "meta-app" {
		t.Errorf("AppName = %q, want meta-app（meta.json name 兜底）", info2.AppName)
	}

	// 两者都缺时用包文件名（去扩展名）
	zipPath3 := writeTestZip(t, "file-fallback-app.zip",
		zipEntrySpec{name: "artifact.toml", data: []byte("# no name\n")},
	)
	info3, err := readArtifactFromZip(zipPath3)
	if err != nil {
		t.Fatalf("readArtifactFromZip failed: %v", err)
	}
	if info3.AppName != "file-fallback-app" {
		t.Errorf("AppName = %q, want file-fallback-app（包文件名兜底）", info3.AppName)
	}
}

// rune 长度校验：name≤64、description≤200，超限报错；中文按 rune 计而非字节。
func TestValidateProjectAppMetaLength(t *testing.T) {
	cases := []struct {
		name        string
		description string
		wantErr     error
	}{
		{"", "", nil},
		{strings.Repeat("a", 64), strings.Repeat("d", 200), nil},
		{strings.Repeat("中", 64), "", nil}, // 64 个汉字（192 字节）按 rune 计应通过
		{"", strings.Repeat("说", 200), nil},
		{strings.Repeat("a", 65), "", domainproject.ErrAppNameTooLong},
		{strings.Repeat("中", 65), "", domainproject.ErrAppNameTooLong},
		{"", strings.Repeat("d", 201), domainproject.ErrAppDescTooLong},
		{"", strings.Repeat("说", 201), domainproject.ErrAppDescTooLong},
	}
	for i, tc := range cases {
		err := validateProjectAppMetaLength(tc.name, tc.description)
		if tc.wantErr == nil {
			if err != nil {
				t.Errorf("case %d: got %v, want nil", i, err)
			}
			continue
		}
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("case %d: got %v, want %v", i, err, tc.wantErr)
		}
	}
}

// 导入生效描述：用户提交值（trim 后非空）优先，空或纯空白回退包内 meta 描述（与 appName 覆盖规则一致）。
func TestResolveImportAppDescription(t *testing.T) {
	cases := []struct {
		req  string
		meta string
		want string
	}{
		{"用户描述", "包内描述", "用户描述"}, // 用户值优先
		{"", "包内描述", "包内描述"},     // 空回退 meta
		{"  ", "包内描述", "包内描述"},   // 纯空白回退 meta
		{"", "", ""},             // 两者皆空
	}
	for i, tc := range cases {
		if got := resolveImportAppDescription(tc.req, tc.meta); got != tc.want {
			t.Errorf("case %d: got %q, want %q", i, got, tc.want)
		}
	}
}

// 静态分支读路径的尺寸上限：CD 中声明的单文件尺寸超限时，在读取前即按包格式错误拒绝。
func TestReadProjectAppPackageFromPath_RejectsOversizedEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "big.zip")
	writeDeclaredOversizedZip(t, zipPath)

	_, err := readProjectAppPackageFromPath(zipPath, "big.zip")
	if !errors.Is(err, domainproject.ErrUnsupportedPackageFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedPackageFormat", err)
	}
}

// writeDeclaredOversizedZip 手工构造一个 CD 声明单文件 uncompressed size > 2GiB、
// 但实际内容极小的 zip（仅含根目录 meta.json），用于验证读取前的声明尺寸上限拦截。
func writeDeclaredOversizedZip(t *testing.T, zipPath string) {
	t.Helper()
	le := binary.LittleEndian
	var raw bytes.Buffer

	// local file header + data（Store，实际内容 "{}"）
	putU32 := func(v uint32) { var b [4]byte; le.PutUint32(b[:], v); raw.Write(b[:]) }
	putU16 := func(v uint16) { var b [2]byte; le.PutUint16(b[:], v); raw.Write(b[:]) }
	putU32(0x04034b50) // local file header signature
	putU16(20)         // version needed
	putU16(0)          // flags
	putU16(0)          // method: store
	putU16(0)          // mod time
	putU16(0)          // mod date
	putU32(0)          // crc32
	putU32(2)          // compressed size
	putU32(2)          // uncompressed size
	putU16(uint16(len("meta.json")))
	putU16(0) // extra len
	raw.WriteString("meta.json")
	raw.WriteString("{}")
	localSize := raw.Len()

	// central directory entry：uncompressed size 谎报为 maxPackageFileSize+1（仍能装入 uint32）
	cd := new(bytes.Buffer)
	putU32CD := func(v uint32) { var b [4]byte; le.PutUint32(b[:], v); cd.Write(b[:]) }
	putU16CD := func(v uint16) { var b [2]byte; le.PutUint16(b[:], v); cd.Write(b[:]) }
	putU32CD(0x02014b50)                     // central directory signature
	putU16CD(20)                             // version made by
	putU16CD(20)                             // version needed
	putU16CD(0)                              // flags
	putU16CD(0)                              // method: store
	putU16CD(0)                              // mod time
	putU16CD(0)                              // mod date
	putU32CD(0)                              // crc32
	putU32CD(2)                              // compressed size
	putU32CD(uint32(maxPackageFileSize) + 1) // uncompressed size（声明超限）
	putU16CD(uint16(len("meta.json")))
	putU16CD(0)           // extra len
	putU16CD(0)           // comment len
	putU16CD(0)           // disk number start
	putU16CD(0)           // internal attrs
	putU32CD(0o644 << 16) // external attrs
	putU32CD(0)           // local header offset
	cd.WriteString("meta.json")

	// EOCD
	putU32(0x06054b50) // EOCD signature
	putU16(0)          // disk number
	putU16(0)          // cd start disk
	putU16(1)          // entries on this disk
	putU16(1)          // total entries
	putU32(uint32(cd.Len()))
	putU32(uint32(localSize))
	putU16(0) // comment len
	raw.Write(cd.Bytes())

	if err := os.WriteFile(zipPath, raw.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
}

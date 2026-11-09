package appinstall

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"backend/internal/domain/common"
	"github.com/pelletier/go-toml/v2"
)

const maxBundleBytes int64 = 512 << 20
const maxExtractedBytes int64 = 2 << 30
const maxBundleFiles = 50000
const maxMetadataBytes int64 = 1 << 20

var packageScriptName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// RuntimeManifest describes the portable subset executed by the native driver.
// Legacy Docker/project declarations are never executed here.
type RuntimeManifest struct {
	Entry          string `json:"entry,omitempty"`
	BuildScript    string `json:"buildScript,omitempty"`
	StartScript    string `json:"startScript,omitempty"`
	HealthPath     string `json:"healthPath,omitempty"`
	SourceDir      string `json:"sourceDir,omitempty"`
	PackageManager string `json:"packageManager,omitempty"`
	DependencyMode string `json:"dependencyMode,omitempty"`
}
type Bundle struct {
	InvalidRoles bool               `json:"invalidRoles,omitempty"`
	Roles        []BundleRole       `json:"-"`
	RolePlan     *RolePlan          `json:"rolePlan,omitempty"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Version      string             `json:"version,omitempty"`
	SourceAppID  string             `json:"sourceAppId,omitempty"`
	PackageName  string             `json:"packageName,omitempty"`
	ExistingApp  *ExistingApp       `json:"existingApp,omitempty"`
	Runtime      RuntimeManifest    `json:"runtime"`
	Ignored      []string           `json:"ignoredResources"`
	Metadata     *BundleMetadata    `json:"metadata,omitempty"`
	MetadataPlan []MetadataPlanItem `json:"metadataPlan,omitempty"`
	root         string
	files        []*zip.File
}

func safeBundlePath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || !utf8.ValidString(name) {
		return "", errors.New("invalid bundle path")
	}
	trimmed := strings.TrimSuffix(name, "/")
	clean := path.Clean(trimmed)
	if clean == "." || clean != trimmed || strings.HasPrefix(clean, "../") {
		return "", errors.New("relative bundle path is forbidden")
	}
	for _, r := range clean {
		if r < 32 {
			return "", errors.New("control characters in App path")
		}
	}
	for _, p := range strings.Split(clean, "/") {
		if strings.TrimRight(p, " .") != p || strings.ContainsAny(p, "<>\"|?*") {
			return "", errors.New("Windows-incompatible bundle path")
		}
		stem := strings.ToUpper(strings.SplitN(p, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
			return "", errors.New("reserved Windows filename")
		}
	}
	return clean, nil
}
func readSmall(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > uint64(maxMetadataBytes) {
		return nil, errors.New("App metadata exceeds size limit")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxMetadataBytes+1))
	if len(b) > int(maxMetadataBytes) {
		return nil, errors.New("App metadata exceeds size limit")
	}
	return b, err
}

func inspectArchive(z *zip.ReadCloser) (Bundle, error) {
	b := Bundle{Ignored: []string{}}
	if len(z.File) == 0 || len(z.File) > maxBundleFiles {
		return b, errors.New("invalid App file count")
	}
	seen := map[string]bool{}
	var total uint64
	roots := map[string]bool{}
	for _, f := range z.File {
		name, err := safeBundlePath(f.Name)
		if err != nil {
			return b, err
		}
		if f.Mode()&os.ModeType != 0 && !f.FileInfo().IsDir() {
			return b, errors.New("App links and special files are forbidden")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return b, errors.New("duplicate App path")
		}
		seen[key] = true
		if f.UncompressedSize64 > uint64(maxExtractedBytes)-total {
			return b, errors.New("App expands beyond size limit")
		}
		total += f.UncompressedSize64
		if (path.Base(name) == "meta.json" || path.Base(name) == "artifact.toml") && (path.Dir(name) == "." || strings.Count(name, "/") == 1) {
			roots[path.Dir(name)] = true
		}
	}
	if len(roots) != 1 {
		return b, errors.New("App must have one metadata root (meta.json or artifact.toml, optionally inside one wrapper directory)")
	}
	for root := range roots {
		b.root = root
	}
	prefix := ""
	if b.root != "." {
		prefix = b.root + "/"
	}
	var pkg struct {
		Name           string            `json:"name"`
		Scripts        map[string]string `json:"scripts"`
		Engines        map[string]string `json:"engines"`
		PackageManager string            `json:"packageManager"`
	}
	foundPackage := false
	var artifact legacyArtifact
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if f.FileInfo().IsDir() && name == b.root {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			return b, errors.New("files outside App package root")
		}
		rel := strings.TrimPrefix(name, prefix)
		if f.FileInfo().IsDir() && name == b.root {
			continue
		}
		b.files = append(b.files, f)
		if rel == "metadata.json" {
			raw, err := readSmall(f)
			if err != nil {
				return b, err
			}
			b.Metadata, err = parseBundleMetadata(raw)
			if err != nil {
				return b, err
			}
		}
		if rel == "meta.json" {
			raw, err := readSmall(f)
			if err != nil {
				return b, err
			}
			var meta struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Version     json.RawMessage `json:"version"`
				AppID       string          `json:"appId"`
				Runtime     RuntimeManifest `json:"runtime"`
			}
			if json.Unmarshal(raw, &meta) != nil {
				return b, errors.New("invalid meta.json")
			}
			b.Name = meta.Name
			b.Description = meta.Description
			if len(meta.Version) > 0 && string(meta.Version) != "null" {
				if json.Unmarshal(meta.Version, &b.Version) != nil {
					var version json.Number
					if json.Unmarshal(meta.Version, &version) != nil {
						return b, errors.New("invalid App version")
					}
					b.Version = version.String()
				}
			}
			b.SourceAppID = meta.AppID
			b.Runtime = meta.Runtime
		}
		if rel == "artifact.toml" {
			raw, err := readSmall(f)
			if err != nil {
				return b, err
			}
			if toml.Unmarshal(raw, &artifact) != nil {
				return b, errors.New("invalid artifact.toml")
			}
		}
		if rel == "flow.json" || rel == "flows.json" || strings.HasPrefix(rel, "flows/") {
			return b, errors.New("legacy Node-RED Flow files are not supported; declare Source and Stream in metadata.json")
		}
		if rel == "roles.json" {
			raw, err := readSmall(f)
			if err != nil {
				return b, err
			}
			b.Roles, err = readBundleRoles(raw)
			b.InvalidRoles = err != nil
		}
		if rel == "uns.json" || rel == "role.json" {
			b.Ignored = append(b.Ignored, rel)
		}
	}
	if err := artifact.apply(&b); err != nil {
		return b, err
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || utf8.RuneCountInString(b.Name) > common.NameMaxLen {
		return b, errors.New("invalid App name")
	}
	if utf8.RuneCountInString(b.Description) > common.DescMaxLen {
		return b, errors.New("invalid App description")
	}
	if b.Runtime.SourceDir == "" {
		// Existing exports may put portable Node sources in a single subdirectory.
		// Do not guess between multiple packages or select dependency packages.
		if !seen[strings.ToLower(prefix+"package.json")] {
			var candidates []string
			for _, f := range b.files {
				rel := strings.TrimPrefix(f.Name, prefix)
				if path.Base(rel) == "package.json" && strings.Count(rel, "/") == 1 && !strings.HasPrefix(rel, "node_modules/") && !strings.HasPrefix(rel, ".git/") {
					candidates = append(candidates, path.Dir(rel))
				}
			}
			if len(candidates) == 1 {
				b.Runtime.SourceDir = candidates[0]
			}
		}
		if b.Runtime.SourceDir == "" {
			b.Runtime.SourceDir = "."
		}
	}
	if b.Runtime.SourceDir != "." {
		if _, err := safeBundlePath(b.Runtime.SourceDir); err != nil {
			return b, err
		}
	}
	sourcePrefix := ""
	if b.Runtime.SourceDir != "." {
		sourcePrefix = b.Runtime.SourceDir + "/"
	}
	for _, f := range b.files {
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == sourcePrefix+"package.json" {
			raw, err := readSmall(f)
			if err != nil {
				return b, err
			}
			if json.Unmarshal(raw, &pkg) != nil {
				return b, errors.New("invalid package.json")
			}
			foundPackage = true
		}
	}
	if !foundPackage {
		return b, errors.New("Node App package.json is missing")
	}
	b.PackageName = strings.TrimSpace(pkg.Name)
	if b.Runtime.Entry != "" {
		if _, err := safeBundlePath(b.Runtime.Entry); err != nil {
			return b, err
		}
		if !seen[strings.ToLower(prefix+sourcePrefix+b.Runtime.Entry)] {
			return b, errors.New("Node entry file is missing")
		}
	}
	if b.Runtime.StartScript == "" && b.Runtime.Entry == "" {
		b.Runtime.StartScript = "start"
	}
	if b.Runtime.StartScript != "" && pkg.Scripts[b.Runtime.StartScript] == "" {
		return b, errors.New("App start script is missing")
	}
	if b.Runtime.BuildScript == "" && pkg.Scripts["build"] != "" {
		b.Runtime.BuildScript = "build"
	}
	if b.Runtime.BuildScript != "" && pkg.Scripts[b.Runtime.BuildScript] == "" {
		return b, errors.New("App build script is missing")
	}
	for _, script := range []string{b.Runtime.StartScript, b.Runtime.BuildScript} {
		if script != "" && !packageScriptName.MatchString(script) {
			return b, errors.New("invalid package script name")
		}
	}
	if b.Runtime.PackageManager == "" {
		b.Runtime.PackageManager = strings.SplitN(pkg.PackageManager, "@", 2)[0]
	}
	if b.Runtime.PackageManager == "" {
		b.Runtime.PackageManager = "npm"
	}
	if b.Runtime.PackageManager != "npm" && b.Runtime.PackageManager != "pnpm" {
		return b, errors.New("unsupported App package manager")
	}
	if b.Runtime.DependencyMode != "" && b.Runtime.DependencyMode != "install" && b.Runtime.DependencyMode != "bundled" {
		return b, errors.New("unsupported App dependency mode")
	}
	if b.Runtime.HealthPath == "" {
		b.Runtime.HealthPath = "/"
	}
	if !strings.HasPrefix(b.Runtime.HealthPath, "/") || strings.HasPrefix(b.Runtime.HealthPath, "//") || strings.ContainsAny(b.Runtime.HealthPath, "\r\n#") {
		return b, errors.New("invalid App health path")
	}
	return b, nil
}
func InspectBundle(filename string) (Bundle, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return Bundle{}, err
	}
	if info.Size() > maxBundleBytes {
		return Bundle{}, errors.New("App ZIP exceeds size limit")
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return Bundle{}, errors.New("invalid App ZIP")
	}
	defer z.Close()
	return inspectArchive(z)
}

// ExtractBundle requires a previously nonexistent staging directory. It never
// extracts into an installed release and removes only its own staging on error.
func ExtractBundle(ctx context.Context, filename, destination string) (b Bundle, err error) {
	info, err := os.Stat(filename)
	if err != nil {
		return b, err
	}
	if info.Size() > maxBundleBytes {
		return b, errors.New("App ZIP exceeds size limit")
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return b, errors.New("invalid App ZIP")
	}
	defer z.Close()
	b, err = inspectArchive(z)
	if err != nil {
		return b, err
	}
	if !filepath.IsAbs(destination) {
		return b, errors.New("App staging path must be absolute")
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return b, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(destination)
		}
	}()
	prefix := ""
	if b.root != "." {
		prefix = b.root + "/"
	}
	var written int64
	for _, f := range b.files {
		if err = ctx.Err(); err != nil {
			return b, err
		}
		rel := strings.TrimPrefix(strings.TrimSuffix(f.Name, "/"), prefix)
		if rel == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			err = os.MkdirAll(target, 0700)
			if err != nil {
				return b, err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return b, err
		}
		input, e := f.Open()
		if e != nil {
			return b, e
		}
		output, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			input.Close()
			return b, e
		}
		n, e := io.Copy(output, io.LimitReader(contextReader{ctx: ctx, reader: input}, maxExtractedBytes-written+1))
		input.Close()
		closeErr := output.Close()
		written += n
		if e != nil {
			return b, e
		}
		if closeErr != nil {
			return b, closeErr
		}
		if written > maxExtractedBytes {
			return b, fmt.Errorf("App exceeds extraction limit")
		}
	}
	return b, nil
}

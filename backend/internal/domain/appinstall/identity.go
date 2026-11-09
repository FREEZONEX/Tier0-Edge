package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/repo"
)

type ExistingApp struct {
	AppID       string `json:"appId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	SamePackage bool   `json:"samePackage"`
}

func packageIdentity(b Bundle) string {
	if b.SourceAppID != "" {
		return "source/" + b.SourceAppID
	}
	if b.PackageName != "" {
		return "package/" + strings.ToLower(b.PackageName)
	}
	return "name/" + b.Name
}

func packageDigest(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, contextReader{ctx: ctx, reader: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Older manifests did not persist package.json's name. Read only the committed
// release's package descriptor; never resolve a path supplied by an upload.
func (m *Management) installedBundle(row repo.AppInstallation) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(row.Manifest, &b); err != nil {
		return b, err
	}
	if b.Name == "" {
		b.Name = row.Name
	}
	if b.PackageName == "" && row.ReleaseID != "" {
		root, err := m.runtime.releasePath(row.AppID, row.ReleaseID)
		if err != nil {
			return b, err
		}
		source := b.Runtime.SourceDir
		if source != "" && source != "." {
			if _, err := safeBundlePath(source); err != nil {
				return b, err
			}
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source), "package.json"))
		if err != nil {
			return b, err
		}
		var pkg struct {
			Name string `json:"name"`
		}
		if err = json.Unmarshal(raw, &pkg); err != nil {
			return b, err
		}
		b.PackageName = strings.TrimSpace(pkg.Name)
	}
	return b, nil
}

func (m *Management) matchingInstall(ctx context.Context, b Bundle, digest string) (*repo.AppInstallation, error) {
	rows, err := repo.NewAppInstallationRepo(ctx).List(ctx)
	if err != nil {
		return nil, err
	}
	var match *repo.AppInstallation
	for i := range rows {
		row := rows[i]
		// Failed first installs have no committed package and use a deterministic ID.
		if row.ReleaseID == "" {
			continue
		}
		old, err := m.installedBundle(row)
		if err != nil {
			return nil, err
		}
		if row.PackageSHA256 != digest && packageIdentity(old) != packageIdentity(b) {
			continue
		}
		if match != nil {
			return nil, invalidRequest("multiple installations already exist for this App; uninstall the duplicate before updating")
		}
		match = &row
	}
	return match, nil
}

func (m *Management) resolveInstall(ctx context.Context, request *InstallRequest) error {
	path, err := m.snapshot(ctx, request.FileID)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	b, err := InspectBundle(path)
	if err != nil {
		return invalidRequest(err.Error())
	}
	digest, err := packageDigest(ctx, path)
	if err != nil {
		return err
	}
	existing, err := m.matchingInstall(ctx, b, digest)
	if err != nil {
		return err
	}
	if existing != nil {
		if request.AppID != "" && request.AppID != existing.AppID {
			return invalidRequest("this App is already installed; replace the existing installation")
		}
		if !request.AllowReplace && existing.PackageSHA256 != digest {
			return invalidRequest("this App is already installed; confirm replacement to update it")
		}
		request.AppID = existing.AppID
	}
	if request.AppID != "" {
		row, err := repo.NewAppInstallationRepo(ctx).Get(ctx, request.AppID)
		if err != nil && !errors.Is(err, repo.ErrNotFound) {
			return err
		}
		if err == nil && row.ReleaseID != "" {
			old, err := m.installedBundle(row)
			if err != nil {
				return err
			}
			if packageIdentity(old) != packageIdentity(b) {
				return invalidRequest("replacement package belongs to a different App")
			}
		}
	}
	request.PackageIdentity = packageIdentity(b)
	return nil
}

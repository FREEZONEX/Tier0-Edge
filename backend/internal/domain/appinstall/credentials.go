package appinstall

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/infra/privatefs"
	"backend/internal/repo"
	"github.com/google/uuid"
)

type Credentials struct {
	root    string
	baseURL string
	aead    cipher.AEAD
	uns     *unsv2.Service
}

// Revoke is restricted to the exact persisted App credential; management
// permission is checked before this internal lifecycle operation is called.
func (c *Credentials) Revoke(ctx context.Context, id string) error {
	value, err := c.read(id)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	keyID, err := strconv.ParseInt(value.KeyID, 10, 64)
	if err != nil || keyID <= 0 {
		return errors.New("invalid saved App credential identity")
	}
	err = repo.NewAPIKeyRepo(ctx).DeleteAppDataKey(ctx, keyID, value.OwnerID, id)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	name, err := c.path(id)
	if err != nil {
		return err
	}
	return os.Remove(name)
}

type appCredential struct {
	KeyID   string `json:"keyId"`
	AppID   string `json:"appId"`
	OwnerID int64  `json:"ownerId"`
	Key     string `json:"apiKey"`
}

func NewCredentials(root, secret, baseURL string, uns *unsv2.Service) (*Credentials, error) {
	if !filepath.IsAbs(root) || len(secret) < 24 || uns == nil {
		return nil, errors.New("native App credential configuration unavailable")
	}
	key := sha256.Sum256([]byte("tier0/native-app/credential/v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Credentials{root: root, baseURL: baseURL, aead: aead, uns: uns}, nil
}
func (c *Credentials) path(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("invalid native App identity")
	}
	return filepath.Join(c.root, id, "credential.enc"), nil
}
func (c *Credentials) read(id string) (appCredential, error) {
	var result appCredential
	name, err := c.path(id)
	if err != nil {
		return result, err
	}
	info, err := os.Lstat(name)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 || !privatefs.IsPrivate(name, info) {
		return result, errors.New("native App credential file permissions invalid")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return result, err
	}
	n := c.aead.NonceSize()
	if len(data) < n {
		return result, errors.New("native App credential file invalid")
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], []byte(id))
	if err != nil {
		return result, errors.New("native App credential decryption failed")
	}
	if json.Unmarshal(plain, &result) != nil || result.AppID != id || result.Key == "" {
		return result, errors.New("native App credential identity mismatch")
	}
	return result, nil
}
func (c *Credentials) Ensure(ctx context.Context, id string) error {
	name, err := c.path(id)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(name); err == nil {
		_, err = c.Environment(ctx, id)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	return c.issue(ctx, id, false)
}

// Refresh rotates only a stopped installation. Management serializes this with
// install/start/stop; ordinary Ensure deliberately keeps the existing snapshot.
func (c *Credentials) Refresh(ctx context.Context, id string) error {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" || subject.APIKeyID != 0 || subject.APIKeyAppID != "" || subject.MCPRestricted || subject.FleetTask != nil || !contextx.HasResource(subject, "uns.app.manage") || !contextx.HasResource(subject, "uns.app.credentials") {
		return forbiddenRequest("App credential refresh requires a platform credential manager")
	}
	if _, err := c.path(id); err != nil {
		return err
	}
	row, err := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
	if err != nil {
		return err
	}
	if row.ReleaseID == "" || row.Stage == "uninstalled" || row.OperationID != "" || row.Status != "stopped" || row.DesiredState != "stopped" {
		return invalidRequest("stop the installed App before refreshing its credentials")
	}
	if _, err := c.read(id); err != nil && !os.IsNotExist(err) {
		return err
	}
	return c.issue(ctx, id, true)
}

func (c *Credentials) issue(ctx context.Context, id string, replace bool) error {
	name, err := c.path(id)
	if err != nil {
		return err
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" || subject.UserID == 0 {
		return errors.New("App credential issuance requires an authenticated platform user")
	}
	// Names are unique even after soft deletion; every issuance has its own ID.
	request, _ := json.Marshal(unsv2.AppKeyRequest{AppID: id, Name: "native-" + uuid.NewString()})
	value, err := c.uns.Execute(ctx, "appKeyCreate", request)
	if err != nil {
		return err
	}
	created, ok := value.(unsv2.AppKeyCreated)
	if !ok {
		return errors.New("App credential response invalid")
	}
	saved := false
	defer func() {
		if !saved {
			// Cleanup is tied to the exact candidate we just created, including
			// cancellation or loss of the issuer's subsequent management access.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			keyID, _ := strconv.ParseInt(created.KeyID, 10, 64)
			_ = repo.NewAPIKeyRepo(cleanup).DeleteAppDataKey(cleanup, keyID, subject.UserID, id)
		}
	}()
	plain, err := json.Marshal(appCredential{KeyID: created.KeyID, AppID: id, OwnerID: subject.UserID, Key: created.APIKey})
	if err != nil {
		return err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := c.aead.Seal(nonce, nonce, plain, []byte(id))
	dir := filepath.Dir(name)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !privatefs.IsPrivate(dir, info) {
		return errors.New("native App credential directory permissions invalid")
	}
	file, err := os.CreateTemp(dir, "credential-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(encrypted); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if replace {
		// The candidate is fully written first. After revocation a failed rename
		// leaves the App stopped without credentials; an explicit retry recovers.
		if err = c.Revoke(ctx, id); err != nil {
			return err
		}
	}
	if err = os.Rename(file.Name(), name); err != nil {
		return err
	}
	saved = true
	return privatefs.SyncDir(dir)
}
func (c *Credentials) Environment(ctx context.Context, id string) (map[string]string, error) {
	value, err := c.read(id)
	if err != nil {
		return nil, err
	}
	key, err := repo.NewAPIKeyRepo(ctx).ValidateAPIKeyDetail(ctx, value.Key)
	if err != nil || key.AppID != id || key.OwnerID != value.OwnerID || key.UsageType != repo.APIKeyUsageAppData {
		return nil, errors.New("native App credential is unavailable or revoked")
	}
	return map[string]string{"TIER0_API_KEY": value.Key, "TIER0_API_HOST": c.baseURL, "TIER0_UNS_APP_ID": id}, nil
}

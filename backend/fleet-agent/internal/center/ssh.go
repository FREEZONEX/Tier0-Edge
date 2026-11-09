package center

import (
	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
	"bytes"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
)

// LocalPort is installer state, not part of the node-to-center protocol. The
// center always uses Port on the overlay, even when the local listener moves.
type installedSSHManifest struct {
	fleetprotocol.SSHAdvertisement
	LocalPort int  `json:"localPort"`
	Forward   bool `json:"-"`
}

func readSSHManifest(cfg config.Config) (installedSSHManifest, error) {
	var result installedSSHManifest
	st, err := os.Lstat(cfg.SSHManifestFile)
	if err != nil || !st.Mode().IsRegular() || !safeSSHPath(cfg.SSHManifestFile, st, false) || st.Size() > 32768 {
		return result, errors.New("SSH_INSTALLATION_REQUIRED")
	}
	raw, err := os.ReadFile(cfg.SSHManifestFile)
	if err != nil || json.Unmarshal(raw, &result) != nil {
		return result, errors.New("SSH_INSTALLATION_INVALID")
	}
	result.Forward = result.LocalPort != 0
	if !result.Forward {
		result.LocalPort = result.Port // Existing installer manifests.
	}
	if result.Port < 1 || result.Port > 65535 || result.Port == fleetprotocol.DefaultNetworkIngressPort || result.LocalPort < 1 || result.LocalPort > 65535 || result.LocalPort == fleetprotocol.DefaultNetworkIngressPort {
		return result, errors.New("SSH_INSTALLATION_INVALID")
	}
	return result, nil
}

// Installer-owned public manifest; no host private key is read by the Agent.
func sshAdvertisement(cfg config.Config) *fleetprotocol.SSHAdvertisement {
	manifest, err := readSSHManifest(cfg)
	if err != nil {
		return &fleetprotocol.SSHAdvertisement{ErrorCode: err.Error()}
	}
	result := &manifest.SSHAdvertisement
	if cfg.NetworkMode == fleetprotocol.NetworkModeDirect {
		result.Port = manifest.LocalPort
	}
	result.TrustedCAFingerprint = ""
	raw, err := os.ReadFile(filepath.Join(cfg.DataDir, "ssh", "trusted-ca.pub"))
	if err == nil {
		if key, _, _, _, e := ssh.ParseAuthorizedKey(raw); e == nil {
			result.TrustedCAFingerprint = ssh.FingerprintSHA256(key)
		}
	}
	return result
}
func installSSHTrust(cfg config.Config, public string) (bool, error) {
	key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(public))
	if err != nil || len(rest) > 0 || len(public) > 16384 {
		return false, errors.New("SSH CA public key invalid")
	}
	if _, cert := key.(*ssh.Certificate); cert {
		return false, errors.New("SSH CA must be a plain public key")
	}
	dir := filepath.Join(cfg.DataDir, "ssh")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || !safeSSHPath(dir, st, true) {
		return false, errors.New("SSH trust directory is unsafe")
	}
	path := filepath.Join(dir, "trusted-ca.pub")
	if st, e := os.Lstat(path); e == nil && (!st.Mode().IsRegular() || !safeSSHPath(path, st, true)) {
		return false, errors.New("SSH trust file is unsafe")
	}
	raw := ssh.MarshalAuthorizedKey(key)
	current, _ := os.ReadFile(path)
	if bytes.Equal(current, raw) {
		return false, nil
	}
	tmp, err := os.CreateTemp(dir, ".ca-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		return false, err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	tmp.Close()
	if err = os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

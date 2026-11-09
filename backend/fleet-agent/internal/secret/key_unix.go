//go:build !windows

package secret

import (
	"errors"
	"fmt"
	"os"
)

func loadOrCreatePlatformKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
			return nil, fmt.Errorf("secure master key permissions: %w", chmodErr)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err = generateMasterKey()
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreatePlatformKey(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

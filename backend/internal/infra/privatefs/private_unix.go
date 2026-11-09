//go:build !windows

package privatefs

import "os"

// IsPrivate checks an owner-private file or directory without following links.
func IsPrivate(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0077 == 0
}

func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

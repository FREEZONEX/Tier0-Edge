//go:build !windows

package center

import "os"

func safeSSHPath(_ string, st os.FileInfo, private bool) bool {
	mask := os.FileMode(0022)
	if private {
		mask = 0077
	}
	return st.Mode().Perm()&mask == 0
}

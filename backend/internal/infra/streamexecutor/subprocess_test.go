package streamexecutor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// Keep Unix socket paths below the OS limit, including Windows' longer temp root.
func subprocessPaths(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "t0-exec-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	name := "stream-executor"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return dir, filepath.Join(dir, name)
}

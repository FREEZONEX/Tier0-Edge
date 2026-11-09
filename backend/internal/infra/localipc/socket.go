// Package localipc contains platform-aware checks shared by private runtimes.
package localipc

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

// StaleSocket permits removal only when a private Unix socket is absent or its
// peer positively refused the connection. Windows reports Winsock error 10061.
func StaleSocket(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) ||
		(runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(10061)))
}

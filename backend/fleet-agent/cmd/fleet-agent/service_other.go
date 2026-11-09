//go:build !windows

package main

import "log/slog"

func runAsSystemService(_ *slog.Logger) (bool, error) {
	return false, nil
}

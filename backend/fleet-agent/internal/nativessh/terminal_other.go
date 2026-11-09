//go:build !windows && !linux && !darwin

package nativessh

import "errors"

func supported() error                                       { return errors.New("SSH_NATIVE_PLATFORM_UNSUPPORTED") }
func startTerminal(uint32, uint32, string) (terminal, error) { return nil, supported() }

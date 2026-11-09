package cluster

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// Only stable codes cross the API boundary; SSH/credential errors may contain
// private deployment details and must never be returned verbatim.
type FleetSSHConnectError struct{ Code string }

func (e *FleetSSHConnectError) Error() string { return e.Code }
func (e *FleetSSHConnectError) Unwrap() error { return ErrFleetSSHConnection }

func sshFailure(code string) error {
	if code == "" {
		code = "SSH_CONNECTION_FAILED"
	}
	return &FleetSSHConnectError{Code: code}
}

func sshFailureCode(err error) string {
	var failure *FleetSSHConnectError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return "SSH_CONNECTION_FAILED"
}

func sshTransportFailure(err error, handshake bool) error {
	var failure *FleetSSHConnectError
	if errors.As(err, &failure) {
		return failure
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return sshFailure("SSH_TIMEOUT")
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return sshFailure("SSH_PORT_REFUSED")
	}
	if !handshake {
		return sshFailure("SSH_NETWORK_UNAVAILABLE")
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return sshFailure("SSH_AUTH_REJECTED")
	}
	return sshFailure("SSH_HANDSHAKE_FAILED")
}

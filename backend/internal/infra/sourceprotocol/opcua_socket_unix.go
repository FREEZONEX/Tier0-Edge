//go:build !windows

package sourceprotocol

import (
	"context"
	"net"
)

func dialOPCUA(ctx context.Context, socket string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}

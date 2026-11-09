package sourceprotocol

import (
	"context"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

func dialOPCUA(ctx context.Context, socket string) (net.Conn, error) {
	if strings.HasPrefix(socket, `\\.\pipe\`) {
		return winio.DialPipeContext(ctx, socket)
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}

//go:build !windows

package sourceprotocol

import "context"

func serialDevices(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return unixSerialDevices()
}

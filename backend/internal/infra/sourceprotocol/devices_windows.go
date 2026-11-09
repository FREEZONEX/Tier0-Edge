package sourceprotocol

import (
	"context"
	"errors"
	"golang.org/x/sys/windows/registry"
	"sort"
)

func serialDevices(ctx context.Context) ([]string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil, err
	}
	ports := []string{}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		port, _, e := key.GetStringValue(name)
		if e != nil {
			return nil, e
		}
		ports = append(ports, port)
	}
	sort.Strings(ports)
	return ports, nil
}

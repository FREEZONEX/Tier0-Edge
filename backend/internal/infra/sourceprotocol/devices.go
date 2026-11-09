package sourceprotocol

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"backend/internal/domain/sourcestream"
)

func (r *Runtime) Devices(ctx context.Context, kind string) sourcestream.PhysicalInventory {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := sourcestream.PhysicalInventory{}
	if kind == "" || kind == "usb-hid" {
		devices, err := r.HIDDevices(ctx)
		if err != nil {
			out.Errors = append(out.Errors, sourcestream.DeviceDiscoveryError{Kind: "usb-hid", Code: "DISCOVERY_UNAVAILABLE"})
		}
		for _, d := range devices {
			out.Devices = append(out.Devices, sourcestream.PhysicalDevice{Kind: "usb-hid", Label: d.Label, Address: d.Endpoint, LegacyAddress: d.LegacyEndpoint})
		}
	}
	if kind == "" || kind == "serial" {
		ports, err := serialDevices(ctx)
		if err != nil {
			out.Errors = append(out.Errors, sourcestream.DeviceDiscoveryError{Kind: "serial", Code: "DISCOVERY_UNAVAILABLE"})
		}
		for _, p := range ports {
			out.Devices = append(out.Devices, sourcestream.PhysicalDevice{Kind: "serial", Label: p, Address: p})
		}
	}
	return out
}

func unixSerialDevices() ([]string, error) {
	patterns := []string{"/dev/serial/by-id/*"}
	if runtime.GOOS == "darwin" {
		patterns = []string{"/dev/cu.usb*", "/dev/cu.wch*", "/dev/cu.SLAB*"}
	}
	ports := []string{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		ports = append(ports, matches...)
	}
	sort.Strings(ports)
	return ports, nil
}

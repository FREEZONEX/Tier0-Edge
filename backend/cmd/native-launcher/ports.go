//go:build windows || darwin || linux

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Only the Linux entry exposes this first-install option. Existing installations
// always use their persisted setting; inherited environment is never consulted.
var requestedPortOffset = -1
var servicePortOffset int

var nativePorts = []string{"18080", "15432", "14222", "11883", "18083", "19732", "19443", "19733"}

func servicePort(original string) string {
	n, _ := strconv.Atoi(original)
	return strconv.Itoa(n + servicePortOffset)
}

func mapServicePorts(value string) string {
	for _, p := range nativePorts {
		if value == p {
			return servicePort(p)
		}
	}
	pairs := []string{}
	for _, p := range nativePorts {
		pairs = append(pairs, ":"+p, ":"+servicePort(p))
	}
	return strings.NewReplacer(pairs...).Replace(value)
}

func loadServicePorts(root string) (func() error, error) {
	name := filepath.Join(root, "state", "ports.json")
	raw, err := os.ReadFile(name)
	exists := err == nil
	var state struct {
		Offset int `json:"offset"`
	}
	if exists {
		if err = json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if state.Offset < 0 || state.Offset > 40000 || requestedPortOffset < -1 || requestedPortOffset > 40000 {
		return nil, fmt.Errorf("port offset must be between 0 and 40000")
	}
	changed := requestedPortOffset >= 0 && requestedPortOffset != state.Offset
	if changed {
		// Older launchers saved the offset before preflight. Allow a retry on
		// such a data root only if initialization has never started.
		for _, marker := range []string{"environment.json", "install-state.json", "upgrade-pending.json"} {
			if _, err := os.Stat(filepath.Join(root, "state", marker)); err == nil {
				return nil, fmt.Errorf("port offset differs from installed configuration")
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		state.Offset = requestedPortOffset
	}
	servicePortOffset = state.Offset
	if requestedPortOffset < 0 || (exists && !changed) {
		return nil, nil
	}
	return func() error { return writeJSONAtomic(name, state) }, nil
}

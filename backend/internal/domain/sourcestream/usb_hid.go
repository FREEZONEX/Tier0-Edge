package sourcestream

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var windowsHID = regexp.MustCompile(`(?i)^usb-hid://windows/(VID_[0-9a-f]{4}&PID_[0-9a-f]{4}(?:&[a-z0-9_]+)*)/([a-z0-9_&-]{1,200})$`)
var hidKey = regexp.MustCompile(`^usb-hid://device/[a-f0-9]{64}$`)

func IsHIDDeviceKey(endpoint string) bool { return hidKey.MatchString(endpoint) }

type HIDDevice struct {
	Endpoint       string `json:"endpoint"`
	Label          string `json:"label"`
	LegacyEndpoint string `json:"legacyEndpoint"`
}

type HIDPreview struct {
	Received bool   `json:"received"`
	Text     string `json:"text,omitempty"`
}

func (s *Service) HIDDevices(ctx context.Context) ([]HIDDevice, error) {
	host, ok := s.runtime.(interface {
		HIDDevices(context.Context) ([]HIDDevice, error)
	})
	if !ok {
		return nil, ErrUnavailable
	}
	return host.HIDDevices(ctx)
}

func (s *Service) PreviewHID(ctx context.Context, endpoint string, timeoutMs int) (HIDPreview, error) {
	if !IsHIDDeviceKey(endpoint) || timeoutMs < 1000 || timeoutMs > 15000 {
		return HIDPreview{}, invalid("device", "invalid")
	}
	host, ok := s.runtime.(interface {
		PreviewHID(context.Context, string, int) (HIDPreview, error)
	})
	if !ok {
		return HIDPreview{}, ErrUnavailable
	}
	return host.PreviewHID(ctx, endpoint, timeoutMs)
}

// HID endpoints pin a single physical USB port as well as vendor and product.
// Vendor/product alone would also select a second identical keyboard.
func (d SourceDefinition) HIDDevice() ([]string, error) {
	if match := windowsHID.FindStringSubmatch(d.Endpoint); match != nil {
		return []string{"HID\\" + match[1] + "\\" + match[2]}, nil
	}
	const prefix = "usb-hid://device/"
	device := make([]string, 3)
	if !strings.HasPrefix(d.Endpoint, prefix) {
		return device, invalid("endpoint", "invalid")
	}
	parts := strings.Split(strings.TrimPrefix(d.Endpoint, prefix), "/")
	if len(parts) != len(device) {
		return device, invalid("endpoint", "invalid")
	}
	for i, s := range parts {
		bits := 16
		if i == 2 {
			bits = 32
		}
		n, err := strconv.ParseUint(s, 16, bits)
		if err != nil || n == 0 {
			return device, invalid("endpoint", "invalid")
		}
		device[i] = strconv.FormatUint(n, 10)
	}
	return device, nil
}

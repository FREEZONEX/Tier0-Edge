package ha

import (
	"context"
	"errors"
	"net"
	"strings"
)

type VIPChecker interface {
	Owns(context.Context, string, string) (bool, error)
}

type NetworkVIPChecker struct{}

func (NetworkVIPChecker) Owns(_ context.Context, interfaceName, vip string) (bool, error) {
	interfaceName = strings.TrimSpace(interfaceName)
	vip = strings.TrimSpace(vip)
	if interfaceName == "" || net.ParseIP(vip) == nil {
		return false, errors.New("invalid Fleet HA interface or VIP")
	}
	device, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return false, err
	}
	addresses, err := device.Addrs()
	if err != nil {
		return false, err
	}
	target := net.ParseIP(vip)
	for _, address := range addresses {
		var current net.IP
		switch value := address.(type) {
		case *net.IPNet:
			current = value.IP
		case *net.IPAddr:
			current = value.IP
		default:
			host, _, splitErr := net.SplitHostPort(address.String())
			if splitErr == nil {
				current = net.ParseIP(host)
			}
		}
		if current != nil && current.Equal(target) {
			return true, nil
		}
	}
	return false, nil
}

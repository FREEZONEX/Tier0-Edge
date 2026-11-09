package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"backend/internal/repo"
)

// Device is a local hardware observation, independent of a configured Source.
type Device struct {
	VendorID           string         `json:"vendorId,omitempty"`
	ProductID          string         `json:"productId,omitempty"`
	Presence           string         `json:"presence"`
	IdentityScope      string         `json:"identityScope"`
	Classification     string         `json:"classification"`
	DeviceID           string         `json:"deviceId"`
	Kind               string         `json:"kind"`
	Label              string         `json:"label"`
	Address            string         `json:"address"`
	Protocol           string         `json:"protocol,omitempty"`
	CanCreateSource    bool           `json:"canCreateSource"`
	RequiredParameters []string       `json:"requiredParameters"`
	Sources            []DeviceSource `json:"sources"`
}
type DeviceSource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Health string `json:"health"`
}
type DeviceInventory struct {
	Devices          []Device               `json:"devices"`
	CheckedAt        int64                  `json:"checkedAt"`
	Revision         string                 `json:"revision"`
	Complete         bool                   `json:"complete"`
	Errors           []DeviceDiscoveryError `json:"errors"`
	BindingsComplete bool                   `json:"bindingsComplete"`
}
type DeviceDiscoveryError struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}
type PhysicalDevice struct {
	Kind          string
	Label         string
	Address       string
	LegacyAddress string
}
type PhysicalInventory struct {
	Devices []PhysicalDevice
	Errors  []DeviceDiscoveryError
}

func (s *Service) Devices(ctx context.Context, kind string) (DeviceInventory, error) {
	out := DeviceInventory{Devices: []Device{}, Errors: []DeviceDiscoveryError{}, Complete: true, BindingsComplete: true}
	host, ok := s.runtime.(interface {
		Devices(context.Context, string) PhysicalInventory
	})
	if !ok {
		return out, ErrUnavailable
	}
	physical := host.Devices(ctx, kind)
	out.Errors = append(out.Errors, physical.Errors...)
	out.Complete = len(out.Errors) == 0
	// Bound inventory work and disclose if Source association lookup was truncated.
	rows, total, err := repo.NewSourceStreamRepo(ctx).ListSources(ctx, repo.SourceFilter{Protocol: string(USBHID), Page: 1, Size: 200})
	if err != nil {
		return out, err
	}
	out.BindingsComplete = total <= int64(len(rows))
	for _, p := range physical.Devices {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(p.Kind+"\x00"+p.Address)))
		d := Device{Presence: "connected", IdentityScope: "connection", Classification: "unclassified", DeviceID: digest, Kind: p.Kind, Label: p.Label, Address: p.Address, RequiredParameters: []string{}, Sources: []DeviceSource{}}
		if p.Kind == "usb-hid" {
			if match := windowsHID.FindStringSubmatch(p.LegacyAddress); match != nil {
				d.VendorID = match[1][4:8]
				d.ProductID = match[1][13:17]
			} else if parts, e := (SourceDefinition{Endpoint: p.LegacyAddress}).HIDDevice(); e == nil && len(parts) == 3 {
				vendor, _ := strconv.ParseUint(parts[0], 10, 16)
				product, _ := strconv.ParseUint(parts[1], 10, 16)
				d.VendorID = fmt.Sprintf("%04X", vendor)
				d.ProductID = fmt.Sprintf("%04X", product)
			}
			d.Protocol = string(USBHID)
			d.CanCreateSource = true
			for _, row := range rows {
				var def SourceDefinition
				if json.Unmarshal([]byte(row.Document), &def) != nil {
					continue
				}
				if def.Endpoint == p.Address || def.Endpoint == p.LegacyAddress {
					d.Sources = append(d.Sources, DeviceSource{ID: strconv.FormatInt(row.ID, 10), Name: row.Name, Health: row.Health})
				}
			}
		} else {
			d.RequiredParameters = []string{"protocolAdapter", "baudRate", "dataBits", "parity", "stopBits", "messageFormat"}
		}
		out.Devices = append(out.Devices, d)
	}
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].DeviceID < out.Devices[j].DeviceID })
	data, _ := json.Marshal(out.Devices)
	out.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	out.CheckedAt = time.Now().UnixMilli()
	return out, nil
}

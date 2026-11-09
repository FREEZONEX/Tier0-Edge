// Package sourceresource defines native resources shared by Source discovery
// and immutable Stream publications.
package sourceresource

import "encoding/json"

type AccessModel string

const (
	Publish      AccessModel = "publish"
	Subscribe    AccessModel = "subscribe"
	RequestReply AccessModel = "request-reply"
)

type NativeResource struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Models   []AccessModel   `json:"models"`
	Schema   json.RawMessage `json:"schema,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

type ModbusRange struct {
	ID       string `json:"id"`
	UnitID   *int   `json:"unitId,omitempty"`
	Writable *bool  `json:"writable,omitempty"`
	Table    string `json:"table"`
	Address  int    `json:"address"`
	Quantity int    `json:"quantity"`
}

type ModbusOptions struct {
	UnitID         int           `json:"unitId"`
	PollIntervalMs int           `json:"pollIntervalMs,omitempty"`
	Ranges         []ModbusRange `json:"ranges,omitempty"`
}

func (r ModbusRange) EffectiveUnitID(fallback int) int {
	if r.UnitID != nil {
		return *r.UnitID
	}
	return fallback
}

func (r ModbusRange) CanWrite() bool {
	if r.Writable != nil && !*r.Writable {
		return false
	}
	return (r.Table == "holding-register" && r.Quantity <= 123) || (r.Table == "coil" && r.Quantity <= 1968)
}

func (r ModbusRange) NativeResource(defaultUnitID int) NativeResource {
	models := []AccessModel{Publish, RequestReply}
	writable := r.CanWrite()
	if writable {
		models = append(models, Subscribe)
	}
	return NativeResource{
		ID: "modbus:" + r.ID, Name: r.ID, Models: models,
		Metadata: map[string]any{
			"unitId": r.EffectiveUnitID(defaultUnitID), "table": r.Table,
			"address": r.Address, "quantity": r.Quantity, "writable": writable,
		},
	}
}

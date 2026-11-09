package sourcestream

import (
	"context"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

// ReadReference executes a bounded read against the current Edge-local connection.
// The caller cannot supply an endpoint, credentials, unit ID or driver operation.
func (s *Service) ReadReference(ctx context.Context, id, configRevision int64, selected ModbusRange, timeoutMs int) (any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read") {
		return nil, ErrForbidden
	}
	if id <= 0 || configRevision <= 0 || timeoutMs < 100 || timeoutMs > 10000 || selected.UnitID != nil || selected.Writable != nil || len(selected.ID) > 128 {
		return nil, invalid("read", "invalidReadOnlyRequest")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	r := repo.NewSourceStreamRepo(ctx)
	current, err := r.Source(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.ConfigRevision != configRevision {
		return nil, repo.ErrConflict
	}
	def, credential, _, err := s.connection(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	if def.Protocol != ModbusTCP || def.Modbus == nil || current.Protocol != string(ModbusTCP) {
		return nil, invalid("protocol", "modbusReadOnlyRequired")
	}
	declared := false
	for _, configured := range def.Modbus.Ranges {
		if configured.ID == selected.ID && configured.Table == selected.Table && configured.Address == selected.Address && configured.Quantity == selected.Quantity {
			declared = true
			break
		}
	}
	if !declared {
		return nil, invalid("range", "undeclaredOrChanged")
	}
	if s.runtime == nil {
		return nil, ErrUnavailable
	}
	// Connection loading may decrypt credentials; fence any intervening change
	// immediately before opening the device connection, without holding SQL locks.
	latest, err := r.Source(ctx, id)
	if err != nil {
		return nil, err
	}
	if latest.DesiredState == "disabled" || latest.ConfigRevision != configRevision {
		return nil, repo.ErrConflict
	}
	ctx = context.WithValue(ctx, watchConfigRevisionKey{}, configRevision)
	value, err := s.runtime.Invoke(ctx, id, def, credential, "modbus:"+selected.ID, map[string]any{"operation": "read"})
	if err != nil {
		return nil, ErrExecutionFailed
	}
	return value, nil
}

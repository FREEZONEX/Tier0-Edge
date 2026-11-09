package sourceprotocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/url"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
)

type ModbusDriver struct{ pool *sourceConnectionPool }

var ErrTransportDisconnected = errors.New("SourceStream.transportDisconnected")

type modbusTransportFailure struct{}

func (modbusTransportFailure) Error() string { return ErrProtocol.Error() }
func (modbusTransportFailure) Unwrap() []error {
	return []error{ErrProtocol, ErrTransportDisconnected}
}

func modbusDisconnected(err error) bool {
	// Winsock returns WSAECONNABORTED, WSAECONNRESET or WSAENOTCONN.
	// Go's portable syscall.ECONN* values on Windows use different numbers.
	if runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(10053)) ||
		errors.Is(err, syscall.Errno(10054)) || errors.Is(err, syscall.Errno(10057))) {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ENOTCONN)
}

func safeModbusError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, sourcestream.ErrUnavailable) || errors.Is(err, sourcestream.ErrUnknownResult) {
		return err
	}
	if modbusDisconnected(err) || errors.Is(err, ErrTransportDisconnected) {
		return modbusTransportFailure{}
	}
	return ErrProtocol
}

func (d ModbusDriver) withConnectionPool(pool *sourceConnectionPool) Driver { d.pool = pool; return d }
func (d ModbusDriver) ReleaseSource(id int64) {
	if d.pool != nil {
		d.pool.releaseSource(id)
	}
}
func (d ModbusDriver) Close() {
	if d.pool != nil {
		d.pool.close()
	}
}

func (d ModbusDriver) withClient(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, action func(*modbus.ModbusClient) (any, error)) (any, error) {
	return d.withClientEntry(ctx, id, def, credential, nil, action)
}

func (d ModbusDriver) withClientEntry(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, entry *sourceConnection, action func(*modbus.ModbusClient) (any, error)) (any, error) {
	return d.withClientOperation(ctx, id, def, credential, entry, false, nil, action)
}

func (d ModbusDriver) withReadClient(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, action func(*modbus.ModbusClient) (any, error)) (any, error) {
	return d.withClientOperation(ctx, id, def, credential, nil, true, nil, action)
}

func (d ModbusDriver) withClientOperation(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, entry *sourceConnection, retryRead bool, prepare func(*modbus.ModbusClient) error, action func(*modbus.ModbusClient) (any, error)) (any, error) {
	pool := d.pool
	if pool == nil {
		pool = newSourceConnectionPool()
		defer pool.close()
	}
	if entry == nil {
		var err error
		entry, err = pool.entry(ctx, id, def, credential)
		if err != nil {
			return nil, err
		}
	}
	var fresh atomic.Bool
	connect := func(ctx context.Context) (any, func(), error) {
		client, err := modbusConnection(ctx, def)
		if err != nil {
			return nil, nil, err
		}
		fresh.Store(true)
		return client, func() { _ = client.Close() }, nil
	}
	execute := func(ctx context.Context, value any) (any, error) {
		type result struct {
			value any
			err   error
		}
		finished := make(chan result, 1)
		// The transport has its own finite timeout. Caller cancellation retires
		// this generation; a late write response is never reported as success.
		go func() {
			client := value.(*modbus.ModbusClient)
			// A Source lease can outlive the device's TCP idle timeout. Validate a
			// reused write transport with a scoped read while holding the same gate.
			// Only this read may reconnect; the mutating action is never replayed.
			if prepare != nil && !fresh.Load() {
				err := prepare(client)
				if modbusDisconnected(err) && ctx.Err() == nil {
					next, reconnectErr := pool.replaceValue(ctx, entry, connect)
					if reconnectErr != nil {
						err = reconnectErr
					} else {
						client = next.(*modbus.ModbusClient)
						err = prepare(client)
					}
				}
				if err != nil {
					finished <- result{nil, err}
					return
				}
			}
			output, err := action(client)
			// Retry only a side-effect-free read after a definitive TCP disconnect.
			// Protocol exceptions and timeouts are not reconnect/retry signals.
			if retryRead && modbusDisconnected(err) && ctx.Err() == nil {
				next, reconnectErr := pool.replaceValue(ctx, entry, connect)
				if reconnectErr != nil {
					err = reconnectErr
					if ctx.Err() == nil && !errors.Is(err, sourcestream.ErrUnavailable) {
						err = modbusTransportFailure{}
					}
				} else {
					output, err = action(next.(*modbus.ModbusClient))
				}
			}
			finished <- result{output, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-finished:
			return result.value, result.err
		}
	}
	result, err := pool.withEntry(ctx, entry, connect, execute)
	return result, safeModbusError(err)
}

func modbusConnection(ctx context.Context, def sourcestream.RuntimeSource) (*modbus.ModbusClient, error) {
	if def.Modbus == nil {
		return nil, ErrProtocol
	}
	timeout := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return nil, ctx.Err()
	}
	client, err := modbus.NewClient(&modbus.ClientConfiguration{URL: def.Endpoint, Timeout: timeout, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		return nil, ErrProtocol
	}
	if err = client.SetUnitId(uint8(def.Modbus.UnitID)); err != nil {
		return nil, ErrProtocol
	}
	opened := make(chan error, 1)
	go func() { opened <- client.Open() }()
	select {
	case <-ctx.Done():
		// Open has a library-owned dial timeout. A late successful connection
		// must be closed rather than installed into a canceled Source lease.
		go func() {
			if <-opened == nil {
				_ = client.Close()
			}
		}()
		return nil, ctx.Err()
	case err = <-opened:
		if err != nil {
			return nil, safeModbusError(err)
		}
	}
	return client, nil
}

// Called only inside the Source connection gate: unit switching and the
// protocol exchange are one operation, including after read reconnection.
func readRange(client *modbus.ModbusClient, resource sourcestream.ModbusRange, fallback int) (any, error) {
	unitID := resource.EffectiveUnitID(fallback)
	if unitID < 1 || unitID > 247 {
		return nil, ErrProtocol
	}
	if err := client.SetUnitId(uint8(unitID)); err != nil {
		return nil, ErrProtocol
	}
	switch resource.Table {
	case "holding-register":
		return client.ReadRegisters(uint16(resource.Address), uint16(resource.Quantity), modbus.HOLDING_REGISTER)
	case "input-register":
		return client.ReadRegisters(uint16(resource.Address), uint16(resource.Quantity), modbus.INPUT_REGISTER)
	case "coil":
		return client.ReadCoils(uint16(resource.Address), uint16(resource.Quantity))
	case "discrete-input":
		return client.ReadDiscreteInputs(uint16(resource.Address), uint16(resource.Quantity))
	default:
		return nil, ErrProtocol
	}
}
func (d ModbusDriver) Probe(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) error {
	if def.Modbus == nil {
		return ErrProtocol
	}
	if len(def.Modbus.Ranges) == 0 {
		// Connection-only Sources need a real transport probe, not a fabricated
		// register range. Stream collection owns the optional read scope.
		_, err := d.withClient(ctx, id, def, credential, func(*modbus.ModbusClient) (any, error) { return nil, nil })
		return err
	}
	_, err := d.withReadClient(ctx, id, def, credential, func(client *modbus.ModbusClient) (any, error) {
		return readRange(client, def.Modbus.Ranges[0], def.Modbus.UnitID)
	})
	if err != nil {
		return err
	}
	return nil
}
func (d ModbusDriver) Discover(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, _ string) ([]sourcestream.NativeResource, error) {
	if def.Modbus == nil {
		return nil, ErrProtocol
	}
	result, err := d.withReadClient(ctx, id, def, credential, func(client *modbus.ModbusClient) (any, error) {
		items := []sourcestream.NativeResource{}
		for _, resource := range def.Modbus.Ranges {
			if _, err := readRange(client, resource, def.Modbus.UnitID); err != nil {
				return nil, err
			}
			items = append(items, resource.NativeResource(def.Modbus.UnitID))
		}
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]sourcestream.NativeResource), nil
}
func (d ModbusDriver) Invoke(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, ref string, input any) (any, error) {
	if def.Modbus == nil {
		return nil, ErrProtocol
	}
	var selected *sourcestream.ModbusRange
	for _, resource := range def.Modbus.Ranges {
		if ref == "modbus:"+resource.ID {
			copy := resource
			selected = &copy
			break
		}
	}
	if selected == nil {
		return nil, ErrProtocol
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, ErrProtocol
	}
	var operation struct {
		Operation string          `json:"operation"`
		Values    json.RawMessage `json:"values"`
	}
	if json.Unmarshal(raw, &operation) != nil {
		return nil, ErrProtocol
	}
	if operation.Operation == "write" && !selected.CanWrite() {
		return nil, ErrProtocol
	}
	var attempted atomic.Bool
	withClient := d.withClient
	if operation.Operation == "read" {
		withClient = d.withReadClient
	} else if operation.Operation == "write" {
		withClient = func(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, action func(*modbus.ModbusClient) (any, error)) (any, error) {
			prepare := func(client *modbus.ModbusClient) error {
				_, err := readRange(client, *selected, def.Modbus.UnitID)
				return err
			}
			return d.withClientOperation(ctx, id, def, credential, nil, false, prepare, action)
		}
	}
	result, err := withClient(ctx, id, def, credential, func(client *modbus.ModbusClient) (any, error) {
		if operation.Operation == "read" {
			values, err := readRange(client, *selected, def.Modbus.UnitID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"values": values, "unitId": selected.EffectiveUnitID(def.Modbus.UnitID), "receipt": "response-received", "deviceExecuted": false}, nil
		}
		if operation.Operation != "write" {
			return nil, ErrProtocol
		}
		unitID := selected.EffectiveUnitID(def.Modbus.UnitID)
		if unitID < 1 || unitID > 247 || client.SetUnitId(uint8(unitID)) != nil {
			return nil, ErrProtocol
		}
		var writeErr error
		switch selected.Table {
		case "holding-register":
			var values []uint16
			if json.Unmarshal(operation.Values, &values) != nil || len(values) != selected.Quantity || len(values) > 123 {
				return nil, ErrProtocol
			}
			attempted.Store(true)
			writeErr = client.WriteRegisters(uint16(selected.Address), values)
		case "coil":
			var values []bool
			if json.Unmarshal(operation.Values, &values) != nil || len(values) != selected.Quantity || len(values) > 1968 {
				return nil, ErrProtocol
			}
			attempted.Store(true)
			writeErr = client.WriteCoils(uint16(selected.Address), values)
		default:
			return nil, ErrProtocol
		}
		if writeErr != nil {
			return nil, sourcestream.ErrUnknownResult
		}
		return map[string]any{"receipt": "operation-accepted", "deviceExecuted": false}, nil
	})
	if err != nil && attempted.Load() {
		return nil, sourcestream.ErrUnknownResult
	}
	return result, err
}

func (d ModbusDriver) Watch(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, emit func(Input) error) error {
	defer sourcestream.ReportWatchReady(ctx, false)
	if def.Modbus == nil || emit == nil {
		return ErrProtocol
	}
	// Use a private pool for direct zero-value drivers; registered drivers own
	// one persistent pool shared by Watch and script operations for this Source.
	if d.pool == nil {
		d.pool = newSourceConnectionPool()
		defer d.pool.close()
	}
	entry, err := d.pool.entry(ctx, id, def, credential)
	if err != nil {
		return err
	}
	if err := d.pool.pinWatch(entry); err != nil {
		return err
	}
	defer d.pool.unpinWatch(entry)
	if len(def.Modbus.Ranges) == 0 || def.Modbus.PollIntervalMs < 100 {
		return ErrProtocol
	}
	ticker := time.NewTicker(time.Duration(def.Modbus.PollIntervalMs) * time.Millisecond)
	defer ticker.Stop()
	firstPoll := true
	for {
		for _, resource := range def.Modbus.Ranges {
			if err := ctx.Err(); err != nil {
				return err
			}
			values, err := d.withClientOperation(ctx, id, def, credential, entry, true, nil, func(client *modbus.ModbusClient) (any, error) { return readRange(client, resource, def.Modbus.UnitID) })
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
			if err = emit(Input{ResourceRef: "modbus:" + resource.ID, Data: map[string]any{"values": values, "unitId": resource.EffectiveUnitID(def.Modbus.UnitID), "table": resource.Table, "address": resource.Address}, ReceivedAt: time.Now().UnixMilli()}); err != nil {
				return err
			}
		}
		if firstPoll {
			// A TCP handshake alone does not prove that every selected range
			// can be read with this unit ID and accepted by the ingress sink.
			sourcestream.ReportWatchReady(ctx, true)
			firstPoll = false
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-entry.ctx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return sourcestream.ErrUnavailable
		case <-ticker.C:
		}
	}
}

// 脚本读取使用可取消的 TCP 连接，避免驱动内部固定拨号超时超过调用期限。
func readModbusContext(ctx context.Context, def sourcestream.RuntimeSource, resource sourcestream.ModbusRange) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint, err := url.Parse(def.Endpoint)
	if err != nil || endpoint.Scheme != "tcp" || def.Modbus == nil {
		return nil, ErrProtocol
	}
	fn := map[string]byte{"coil": 1, "discrete-input": 2, "holding-register": 3, "input-register": 4}[resource.Table]
	if fn == 0 || resource.Quantity < 1 {
		return nil, ErrProtocol
	}
	unitID := resource.EffectiveUnitID(def.Modbus.UnitID)
	if unitID < 1 || unitID > 247 {
		return nil, ErrProtocol
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", endpoint.Host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	request := make([]byte, 12)
	binary.BigEndian.PutUint16(request[0:2], 1)
	binary.BigEndian.PutUint16(request[4:6], 6)
	request[6], request[7] = byte(unitID), fn
	binary.BigEndian.PutUint16(request[8:10], uint16(resource.Address))
	binary.BigEndian.PutUint16(request[10:12], uint16(resource.Quantity))
	for len(request) > 0 {
		n, e := conn.Write(request)
		if e != nil {
			return nil, e
		}
		if n == 0 {
			return nil, io.ErrShortWrite
		}
		request = request[n:]
	}
	header := make([]byte, 7)
	if _, err = io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if binary.BigEndian.Uint16(header[0:2]) != 1 || binary.BigEndian.Uint16(header[2:4]) != 0 || header[6] != byte(unitID) || length < 3 || length > 254 {
		return nil, ErrProtocol
	}
	body := make([]byte, length-1)
	if _, err = io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	if body[0] != fn || int(body[1]) != len(body)-2 {
		return nil, ErrProtocol
	}
	if fn <= 2 {
		if len(body)-2 != (resource.Quantity+7)/8 {
			return nil, ErrProtocol
		}
		values := make([]bool, resource.Quantity)
		for i := range values {
			values[i] = body[2+i/8]&(1<<uint(i%8)) != 0
		}
		return values, nil
	}
	if len(body)-2 != resource.Quantity*2 {
		return nil, ErrProtocol
	}
	values := make([]uint16, resource.Quantity)
	for i := range values {
		values[i] = binary.BigEndian.Uint16(body[2+i*2:])
	}
	return values, nil
}

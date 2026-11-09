package fleettunnel

// 验证真实 TLS WebSocket/Yamux、多流方向、鉴权与取消；不连接数据库或设备。
// 认证回调仅用于协议夹具，不代替产品节点凭据、License 和双 KVM 验收。
import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

var tunnelTestIdentity = Identity{Username: "fixture", Password: "test-only-credential", ClientID: "test-client", NodeID: "test-node"}

func tunnelPair(t *testing.T, centerHandler, edgeHandler RPCHandler) (*Service, *Service, ClientConfig) {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fixture", r.URL.Path)
		_, _ = io.WriteString(w, "real edge loopback response")
	}))
	center := New(Options{IsCenter: func() bool { return true },
		Authenticate: func(_ context.Context, identity Identity) (string, error) {
			if identity != tunnelTestIdentity {
				return "", ErrUnauthorized
			}
			return identity.NodeID, nil
		},
		ValidateNode: func(_ context.Context, nodeID string) error {
			if nodeID != tunnelTestIdentity.NodeID {
				return ErrUnauthorized
			}
			return nil
		}})
	center.SetUNSSyncHandler(centerHandler)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !center.TryHandleConnect(w, r) {
			http.NotFound(w, r)
		}
	}))
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	edge := New(Options{LocalBackendAddr: backend.Listener.Addr().String(),
		WebSocketDialer:   &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, HandshakeTimeout: time.Second},
		ReconnectMinDelay: 10 * time.Millisecond, ReconnectMaxDelay: 20 * time.Millisecond,
		LocalDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != backend.Listener.Addr().String() {
				return nil, errors.New("fixture rejected arbitrary proxy destination")
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}})
	edge.SetFleetRPCHandler(edgeHandler)
	t.Cleanup(func() { edge.Close(); center.Close(); server.Close(); backend.Close() })
	config := ClientConfig{CenterBaseURL: server.URL, Identity: tunnelTestIdentity}
	edge.ConfigureClient(config)
	require.Eventually(t, func() bool {
		edge.clientSessionMu.RLock()
		defer edge.clientSessionMu.RUnlock()
		return edge.clientSession != nil && center.Status(config.Identity.NodeID).Status == "online"
	}, 5*time.Second, 10*time.Millisecond)
	return center, edge, config
}

func TestTunnelTLSMultiplexDirectionsAndProbe(t *testing.T) {
	var uploads, infrastructure atomic.Int32
	center, edge, config := tunnelPair(t,
		func(_ context.Context, raw []byte) ([]byte, error) {
			uploads.Add(1)
			require.JSONEq(t, `{"batch":"one"}`, string(raw))
			return []byte(`{"accepted":true}`), nil
		},
		func(_ context.Context, raw []byte) ([]byte, error) {
			infrastructure.Add(1)
			require.JSONEq(t, `{"operation":"health"}`, string(raw))
			return []byte(`{"healthy":true}`), nil
		})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	before := center.Status(config.Identity.NodeID)
	require.NoError(t, edge.Probe(ctx, config))
	require.Equal(t, before.ConnectedAt, center.Status(config.Identity.NodeID).ConnectedAt, "probe must not replace the active session")
	result, err := edge.SendUNSSync(ctx, []byte(`{"batch":"one"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"accepted":true}`, string(result))
	result, err = center.SendFleetRPC(ctx, config.Identity.NodeID, []byte(`{"operation":"health"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"healthy":true}`, string(result))
	connection, err := center.DialContext(ctx, config.Identity.NodeID)
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetDeadline(time.Now().Add(time.Second)))
	request, err := http.NewRequest(http.MethodGet, "http://not-a-dial-target/fixture", nil)
	require.NoError(t, err)
	require.NoError(t, request.Write(connection))
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "real edge loopback response", string(body))
	require.Equal(t, int32(1), uploads.Load())
	require.Equal(t, int32(1), infrastructure.Load())
	config.Identity.Password = "wrong-fixture-credential"
	require.Error(t, edge.Probe(ctx, config))
	require.Equal(t, "online", center.Status(config.Identity.NodeID).Status)
}

func TestTunnelRPCContextCancellation(t *testing.T) {
	for _, direction := range []string{"uns-sync", "fleet-rpc"} {
		t.Run(direction, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			handler := func(context.Context, []byte) ([]byte, error) {
				close(entered)
				<-release
				return []byte(`{"done":true}`), nil
			}
			center, edge, config := tunnelPair(t, handler, handler)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				var err error
				if direction == "uns-sync" {
					_, err = edge.SendUNSSync(ctx, []byte(`{}`))
				} else {
					_, err = center.SendFleetRPC(ctx, config.Identity.NodeID, []byte(`{}`))
				}
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("RPC never entered the actual remote handler")
			}
			cancel()
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(500 * time.Millisecond):
				unblock()
				select {
				case <-result:
				case <-time.After(2 * time.Second):
					t.Fatal("RPC did not drain after releasing the fixture")
				}
				t.Fatal("RPC ignored cancellation after its stream was opened")
			}
			unblock()
			require.Equal(t, "online", center.Status(config.Identity.NodeID).Status, "cancel only this logical stream, not the shared connection")
		})
	}
}

func TestTunnelRPCDeadline(t *testing.T) {
	for _, direction := range []string{laneUNSSync, laneFleetRPC} {
		t.Run(direction, func(t *testing.T) {
			release := make(chan struct{})
			handler := func(context.Context, []byte) ([]byte, error) { <-release; return []byte(`{}`), nil }
			center, edge, config := tunnelPair(t, handler, handler)
			t.Cleanup(func() { close(release) })
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var err error
			if direction == laneUNSSync {
				_, err = edge.SendUNSSync(ctx, []byte(`{}`))
			} else {
				_, err = center.SendFleetRPC(ctx, config.Identity.NodeID, []byte(`{}`))
			}
			require.ErrorIs(t, err, ErrTimeout)
			require.Equal(t, "online", center.Status(config.Identity.NodeID).Status)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, rpcTransportError(ctx, io.EOF), context.Canceled)
	require.ErrorIs(t, rpcTransportError(context.Background(), &net.DNSError{IsTimeout: true}), ErrTimeout)
	require.ErrorIs(t, rpcTransportError(context.Background(), io.EOF), ErrOffline)
}

func TestTunnelUNSLaneFrameLimit(t *testing.T) {
	const unsLimit = 2 * 1024 * 1024
	payload := append([]byte{'{'}, bytes.Repeat([]byte{' '}, unsLimit-1)...)
	payload = append(payload, '}')
	for _, direction := range []string{"sender", "receiver", "fleet-independent"} {
		t.Run(direction, func(t *testing.T) {
			var calls atomic.Int32
			handler := func(context.Context, []byte) ([]byte, error) {
				calls.Add(1)
				return []byte(`{}`), nil
			}
			center, edge, config := tunnelPair(t, handler, handler)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			switch direction {
			case "sender":
				_, err := edge.SendUNSSync(ctx, payload)
				require.Error(t, err, "UNS frame over 2 MiB must be rejected before sending")
				require.Zero(t, calls.Load())
				withinLimit := append(append([]byte(nil), payload[:unsLimit-1]...), '}')
				_, err = edge.SendUNSSync(ctx, withinLimit)
				require.NoError(t, err, "exactly 2 MiB remains within the transport budget")
			case "receiver":
				edge.clientSessionMu.RLock()
				session := edge.clientSession
				edge.clientSessionMu.RUnlock()
				stream, err := session.mux.OpenStream()
				require.NoError(t, err)
				defer stream.Close()
				require.NoError(t, stream.SetDeadline(time.Now().Add(time.Second)))
				require.NoError(t, writeStreamLane(stream, laneUNSSync))
				err = writeFrame(stream, payload)
				if err == nil {
					_, err = readFrame(stream)
				}
				require.Error(t, err, "receiver must independently reject a peer bypassing sender validation")
				require.Zero(t, calls.Load())
			case "fleet-independent":
				_, err := center.SendFleetRPC(ctx, config.Identity.NodeID, payload)
				require.NoError(t, err, "Fleet RPC keeps its existing 4 MiB budget")
				require.Equal(t, int32(1), calls.Load())
			}
			require.Equal(t, "online", center.Status(config.Identity.NodeID).Status)
		})
	}
}

func TestTunnelUNSResponseFrameLimit(t *testing.T) {
	var calls atomic.Int32
	result := append([]byte{'"'}, bytes.Repeat([]byte{'x'}, maxUNSSyncBytes)...)
	result = append(result, '"')
	center, edge, config := tunnelPair(t, func(context.Context, []byte) ([]byte, error) {
		calls.Add(1)
		return result, nil
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := edge.SendUNSSync(ctx, []byte(`{}`))
	require.Error(t, err, "oversized response envelope must not bypass the UNS frame budget")
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "online", center.Status(config.Identity.NodeID).Status)

	connection := &frameTestConn{}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxUNSSyncBytes+1)
	_, err = connection.Buffer.Write(header[:])
	require.NoError(t, err)
	_, err = readFrameBounded(connection, maxUNSSyncBytes)
	require.ErrorIs(t, err, ErrBusy, "reject the length header without reading or allocating its body")
	connection.Reset()
	require.ErrorIs(t, writeFrameBounded(connection, result, maxUNSSyncBytes), ErrBusy)
	require.Zero(t, connection.Len(), "invalid frame must not emit even its header")
}

func TestTunnelEdgeRejectsIncomingOverCapacity(t *testing.T) {
	release, entered := make(chan struct{}), make(chan struct{}, int(maxStreams)+1)
	var calls atomic.Int32
	center, _, config := tunnelPair(t, nil, func(context.Context, []byte) ([]byte, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return []byte(`{}`), nil
	})
	var streams []net.Conn
	t.Cleanup(func() {
		close(release)
		for _, stream := range streams {
			_ = stream.Close()
		}
	})
	center.hubMu.RLock()
	session := center.sessions[config.Identity.NodeID]
	center.hubMu.RUnlock()
	// 绕过发送端计数，证明接收端不会仅依赖对端遵守 64 流约定。
	for index := int32(0); index < maxStreams; index++ {
		stream, err := session.mux.OpenStream()
		require.NoError(t, err)
		streams = append(streams, stream)
		require.NoError(t, writeStreamLane(stream, laneFleetRPC))
		require.NoError(t, writeFrame(stream, []byte(`{}`)))
	}
	for index := int32(0); index < maxStreams; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("admitted stream never reached fixture")
		}
	}
	stream, err := session.mux.OpenStream()
	if err == nil {
		streams = append(streams, stream)
		require.NoError(t, stream.SetDeadline(time.Now().Add(500*time.Millisecond)))
		err = writeStreamLane(stream, laneFleetRPC)
		if err == nil {
			err = writeFrame(stream, []byte(`{}`))
		}
		if err == nil {
			_, err = readFrame(stream)
		}
	}
	require.Error(t, err, "over-capacity stream must be rejected without executing its handler")
	require.Equal(t, maxStreams, calls.Load())
	require.Equal(t, "online", center.Status(config.Identity.NodeID).Status)
}

func TestTunnelHandshakeGuards(t *testing.T) {
	center := New(Options{IsCenter: func() bool { return true }, Authenticate: func(context.Context, Identity) (string, error) { return "known-node", nil }})
	t.Cleanup(center.Close)
	for _, test := range []struct {
		name, method, node       string
		upgrade, protocol, basic bool
		status                   int
	}{
		{"not-upgrade", "GET", "known-node", false, false, false, 426},
		{"wrong-method", "POST", "known-node", true, true, true, 426},
		{"missing-protocol", "GET", "known-node", true, false, true, 426},
		{"missing-auth", "GET", "known-node", true, true, false, 401},
		{"mismatched-node", "GET", "other-node", true, true, true, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, ConnectPath, nil)
			request.Header.Set(nodeIDHeader, test.node)
			if test.upgrade {
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
			}
			if test.protocol {
				request.Header.Set("Sec-WebSocket-Protocol", Subprotocol)
			}
			if test.basic {
				request.SetBasicAuth("fixture", "fixture")
			}
			response := httptest.NewRecorder()
			require.True(t, center.TryHandleConnect(response, request))
			require.Equal(t, test.status, response.Code)
		})
	}
	require.False(t, center.TryHandleConnect(httptest.NewRecorder(), httptest.NewRequest("GET", "/unrelated", nil)))
}

func TestTunnelEndpointsRejectEmbeddedCredentials(t *testing.T) {
	for _, raw := range []string{"https://name:secret@example.invalid", "http://name@example.invalid:8088/base"} {
		_, err := tunnelEndpoint(raw)
		require.Error(t, err, "userinfo must not become part of logged endpoint or a competing Authorization header")
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestTunnelEndpointCandidates(t *testing.T) {
	for _, test := range []struct {
		input  string
		output []string
	}{
		{"https://example.invalid/base?ignored=1#fragment", []string{"wss://example.invalid/base/api/core/fleet-tunnel/connect"}},
		{"http://example.invalid:8088", []string{"ws://example.invalid:8088/api/core/fleet-tunnel/connect", "wss://example.invalid/api/core/fleet-tunnel/connect"}},
		{"http://127.0.0.1:8088", []string{"ws://127.0.0.1:8088/api/core/fleet-tunnel/connect"}},
	} {
		actual, err := tunnelEndpointCandidates(test.input)
		require.NoError(t, err)
		require.Equal(t, test.output, actual)
	}
	_, err := tunnelEndpoint("file:///tmp/not-a-center")
	require.Error(t, err)
}

type frameTestConn struct {
	bytes.Buffer
	chunk int
}

func (c *frameTestConn) Write(p []byte) (int, error) {
	if c.chunk > 0 && len(p) > c.chunk {
		p = p[:c.chunk]
	}
	return c.Buffer.Write(p)
}
func (*frameTestConn) Close() error                     { return nil }
func (*frameTestConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*frameTestConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*frameTestConn) SetDeadline(time.Time) error      { return nil }
func (*frameTestConn) SetReadDeadline(time.Time) error  { return nil }
func (*frameTestConn) SetWriteDeadline(time.Time) error { return nil }

func TestTunnelFrameAndLaneBoundaries(t *testing.T) {
	connection := &frameTestConn{chunk: 2}
	require.NoError(t, writeFrame(connection, []byte(`{"id":1}`)))
	data, err := readFrame(connection)
	require.NoError(t, err)
	require.Equal(t, `{"id":1}`, string(data))
	require.ErrorIs(t, writeFrame(connection, nil), ErrBusy)
	for _, size := range []uint32{0, maxRPCBytes + 1} {
		connection.Reset()
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], size)
		_, err = connection.Buffer.Write(header[:])
		require.NoError(t, err)
		_, err = readFrame(connection)
		require.ErrorIs(t, err, ErrBusy)
	}
	for _, lane := range []string{laneProxy, laneUNSSync, laneFleetRPC} {
		connection.Reset()
		require.NoError(t, writeStreamLane(connection, lane))
		got, err := readStreamLane(connection)
		require.NoError(t, err)
		require.Equal(t, lane, got)
	}
	var count atomic.Int32
	for index := int32(0); index < maxStreams; index++ {
		require.True(t, reserveStream(&count))
	}
	require.False(t, reserveStream(&count))
	require.Equal(t, maxStreams, count.Load())
}

package fleettunnel

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	ConnectPath     = "/api/core/fleet-tunnel/connect"
	ProtocolVersion = "v1"
	Subprotocol     = "tier0-fleet-tunnel-v1"

	clientIDHeader   = "X-Tier0-Client-ID"
	nodeIDHeader     = "X-Tier0-Node-ID"
	probeHeader      = "X-Tier0-Tunnel-Probe"
	maxStreams       = int32(64)
	streamHeader     = "T0TN1:"
	streamHeaderSize = 16
	laneProxy        = "proxy"
	laneUNSSync      = "uns-sync"
	laneFleetRPC     = "fleet-rpc"
	maxRPCBytes      = 4 * 1024 * 1024
	maxUNSSyncBytes  = 2 * 1024 * 1024
)

var (
	ErrOffline      = errors.New("FLEET_TUNNEL_OFFLINE")
	ErrBusy         = errors.New("FLEET_TUNNEL_BUSY")
	ErrTimeout      = errors.New("FLEET_TUNNEL_TIMEOUT")
	ErrUnauthorized = errors.New("FLEET_TUNNEL_UNAUTHORIZED")
)

type Identity struct {
	Username string
	Password string
	ClientID string
	NodeID   string
}

type ClientConfig struct {
	CenterBaseURL string
	Identity      Identity
}

type Status struct {
	Status          string `json:"status"`
	ConnectedAt     int64  `json:"connectedAt,omitempty"`
	LastSeenAt      int64  `json:"lastSeenAt,omitempty"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
}

type Options struct {
	IsCenter          func() bool
	Authenticate      func(context.Context, Identity) (string, error)
	ValidateNode      func(context.Context, string) error
	LocalBackendAddr  string
	WebSocketDialer   *websocket.Dialer
	LocalDialContext  func(context.Context, string, string) (net.Conn, error)
	ReconnectMinDelay time.Duration
	ReconnectMaxDelay time.Duration
}

type RPCHandler func(context.Context, []byte) ([]byte, error)

type Service struct {
	isCenter         func() bool
	authenticate     func(context.Context, Identity) (string, error)
	validateNode     func(context.Context, string) error
	localBackendAddr string
	websocketDialer  *websocket.Dialer
	localDialContext func(context.Context, string, string) (net.Conn, error)
	reconnectMin     time.Duration
	reconnectMax     time.Duration

	hubMu    sync.RWMutex
	sessions map[string]*hubSession

	clientMu        sync.Mutex
	clientCancel    context.CancelFunc
	clientDone      chan struct{}
	clientSessionMu sync.RWMutex
	clientSession   *outboundSession

	handlerMu       sync.RWMutex
	unsSyncHandler  RPCHandler
	fleetRPCHandler RPCHandler

	closed    chan struct{}
	closeOnce sync.Once
}

type outboundSession struct {
	mux       *yamux.Session
	transport net.Conn
	nodeID    string
	streams   atomic.Int32
	closed    atomic.Bool
}

type rpcResponse struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type hubSession struct {
	nodeID      string
	mux         *yamux.Session
	transport   net.Conn
	connectedAt int64
	lastSeenAt  atomic.Int64
	streams     atomic.Int32
	closed      atomic.Bool
}

type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

func New(options Options) *Service {
	dialer := options.WebSocketDialer
	if dialer == nil {
		copy := *websocket.DefaultDialer
		copy.HandshakeTimeout = 10 * time.Second
		dialer = &copy
	}
	localDial := options.LocalDialContext
	if localDial == nil {
		localDial = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	minDelay := options.ReconnectMinDelay
	if minDelay <= 0 {
		minDelay = time.Second
	}
	maxDelay := options.ReconnectMaxDelay
	if maxDelay < minDelay {
		maxDelay = 30 * time.Second
	}
	return &Service{
		isCenter: options.IsCenter, authenticate: options.Authenticate, validateNode: options.ValidateNode,
		localBackendAddr: strings.TrimSpace(options.LocalBackendAddr), websocketDialer: dialer,
		localDialContext: localDial, reconnectMin: minDelay, reconnectMax: maxDelay,
		sessions: make(map[string]*hubSession), closed: make(chan struct{}),
	}
}

func (s *Service) IsCenter() bool {
	return s != nil && s.isCenter != nil && s.isCenter()
}

func (s *Service) SetUNSSyncHandler(handler RPCHandler) {
	if s == nil {
		return
	}
	s.handlerMu.Lock()
	s.unsSyncHandler = handler
	s.handlerMu.Unlock()
}

func (s *Service) SetFleetRPCHandler(handler RPCHandler) {
	if s == nil {
		return
	}
	s.handlerMu.Lock()
	s.fleetRPCHandler = handler
	s.handlerMu.Unlock()
}

// TryHandleConnect intercepts the machine-authenticated WebSocket endpoint.
// It intentionally bypasses browser SessionAuth: the existing collaboration
// credential is verified here and bound back to a non-deleted Fleet node.
func (s *Service) TryHandleConnect(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || (r.URL.Path != ConnectPath && r.URL.Path != ConnectPath+"/") {
		return false
	}
	if s == nil || !s.IsCenter() {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodGet || !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return true
	}
	if !containsSubprotocol(websocket.Subprotocols(r), Subprotocol) {
		http.Error(w, "unsupported tunnel protocol", http.StatusUpgradeRequired)
		return true
	}
	username, password, ok := r.BasicAuth()
	identity := Identity{
		Username: username,
		Password: password,
		ClientID: strings.TrimSpace(r.Header.Get(clientIDHeader)),
		NodeID:   strings.TrimSpace(r.Header.Get(nodeIDHeader)),
	}
	if !ok || s.authenticate == nil {
		http.Error(w, ErrUnauthorized.Error(), http.StatusUnauthorized)
		return true
	}
	nodeID, err := s.authenticate(r.Context(), identity)
	if err != nil || strings.TrimSpace(nodeID) == "" || nodeID != identity.NodeID {
		http.Error(w, ErrUnauthorized.Error(), http.StatusUnauthorized)
		return true
	}

	upgrader := websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{Subprotocol},
		CheckOrigin:      func(*http.Request) bool { return true },
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(probeHeader)), "true") {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe complete"), time.Now().Add(time.Second))
		_ = ws.Close()
		return true
	}
	session := &hubSession{nodeID: nodeID, connectedAt: time.Now().UTC().UnixMilli()}
	session.lastSeenAt.Store(session.connectedAt)
	transport := newWebsocketConn(ws, func() { session.lastSeenAt.Store(time.Now().UTC().UnixMilli()) })
	mux, err := yamux.Server(transport, yamuxConfig())
	if err != nil {
		_ = transport.Close()
		return true
	}
	session.mux = mux
	session.transport = transport
	s.register(session)
	logx.WithContext(r.Context()).Infof("fleet tunnel connected fleetNodeID=%s protocol=%s", nodeID, ProtocolVersion)
	go s.acceptCenterStreams(r.Context(), session)
	<-mux.CloseChan()
	s.unregister(session)
	logx.WithContext(r.Context()).Infof("fleet tunnel disconnected fleetNodeID=%s", nodeID)
	return true
}

func (s *Service) Status(nodeID string) Status {
	if s == nil || !s.IsCenter() {
		return Status{Status: "offline"}
	}
	s.hubMu.RLock()
	session := s.sessions[strings.TrimSpace(nodeID)]
	s.hubMu.RUnlock()
	if session == nil || session.closed.Load() || session.mux.IsClosed() {
		return Status{Status: "offline"}
	}
	return Status{Status: "online", ConnectedAt: session.connectedAt,
		LastSeenAt: session.lastSeenAt.Load(), ProtocolVersion: ProtocolVersion}
}

// Probe verifies the actual WebSocket Upgrade and node credential without
// replacing an established Yamux session for the same node.
func (s *Service) Probe(ctx context.Context, config ClientConfig) error {
	if s == nil || strings.TrimSpace(config.CenterBaseURL) == "" {
		return ErrOffline
	}
	endpoints, err := tunnelEndpointCandidates(config.CenterBaseURL)
	if err != nil {
		return err
	}
	var result error
	for _, endpoint := range endpoints {
		headers := http.Header{}
		headers.Set(clientIDHeader, config.Identity.ClientID)
		headers.Set(nodeIDHeader, config.Identity.NodeID)
		headers.Set(probeHeader, "true")
		headers.Set("Authorization", basicAuthorization(config.Identity.Username, config.Identity.Password))
		dialer := *s.websocketDialer
		dialer.Subprotocols = []string{Subprotocol}
		ws, response, dialErr := dialer.DialContext(ctx, endpoint, headers)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if dialErr != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", endpoint, dialErr))
			if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
				break
			}
			continue
		}
		valid := ws.Subprotocol() == Subprotocol
		_ = ws.Close()
		if valid {
			return nil
		}
		result = errors.Join(result, fmt.Errorf("%s: tunnel protocol negotiation failed", endpoint))
	}
	if result == nil {
		return ErrOffline
	}
	return result
}

// DialContext opens one logical connection to a specific edge node. The edge
// side always relays it to its own loopback Backend; callers cannot select an
// arbitrary destination.
func (s *Service) DialContext(ctx context.Context, nodeID string) (net.Conn, error) {
	if s == nil || !s.IsCenter() {
		return nil, ErrOffline
	}
	nodeID = strings.TrimSpace(nodeID)
	if s.validateNode != nil {
		if err := s.validateNode(ctx, nodeID); err != nil {
			s.DisconnectNode(nodeID)
			return nil, ErrUnauthorized
		}
	}
	s.hubMu.RLock()
	session := s.sessions[nodeID]
	s.hubMu.RUnlock()
	if session == nil || session.closed.Load() || session.mux.IsClosed() {
		return nil, ErrOffline
	}
	for {
		current := session.streams.Load()
		if current >= maxStreams {
			return nil, ErrBusy
		}
		if session.streams.CompareAndSwap(current, current+1) {
			break
		}
	}
	type result struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		stream, err := session.mux.OpenStream()
		resultCh <- result{conn: stream, err: err}
	}()
	select {
	case opened := <-resultCh:
		if opened.err != nil {
			session.streams.Add(-1)
			return nil, ErrOffline
		}
		connection := &countedConn{Conn: opened.conn, release: func() { session.streams.Add(-1) }}
		if err := writeStreamLane(connection, laneProxy); err != nil {
			_ = connection.Close()
			return nil, ErrOffline
		}
		return connection, nil
	case <-ctx.Done():
		session.streams.Add(-1)
		go func() {
			opened := <-resultCh
			if opened.conn != nil {
				_ = opened.conn.Close()
			}
		}()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
}

// SendUNSSync transports one durable, bounded UNS request over an independent
// edge-to-center stream. Persistence and retries remain owned by the UNS worker.
func (s *Service) SendUNSSync(ctx context.Context, payload []byte) ([]byte, error) {
	if s == nil || len(payload) == 0 || len(payload) > maxUNSSyncBytes {
		return nil, ErrOffline
	}
	s.clientSessionMu.RLock()
	session := s.clientSession
	s.clientSessionMu.RUnlock()
	if session == nil || session.closed.Load() || session.mux.IsClosed() {
		return nil, ErrOffline
	}
	if !reserveStream(&session.streams) {
		return nil, ErrBusy
	}
	defer session.streams.Add(-1)
	stream, err := openStream(ctx, session.mux)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	setConnDeadlineFromContext(stream, ctx)
	// Yamux Close 只半关闭写端；截止时间才能唤醒正在等待回执的读取。
	stopCancellation := context.AfterFunc(ctx, func() { _ = stream.SetDeadline(time.Now()) })
	defer stopCancellation()
	if err = writeStreamLane(stream, laneUNSSync); err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	if err = writeFrameBounded(stream, payload, maxUNSSyncBytes); err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	raw, err := readFrameBounded(stream, maxUNSSyncBytes)
	if err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	var response rpcResponse
	if json.Unmarshal(raw, &response) != nil {
		return nil, ErrOffline
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "UNS tunnel request rejected"
		}
		return nil, errors.New(response.Error)
	}
	return append([]byte(nil), response.Payload...), nil
}

// SendFleetRPC transports a bounded infrastructure request from the center
// Backend to one edge Backend. The edge handler owns the fixed operation set;
// callers cannot select a destination or open an arbitrary proxy.
func (s *Service) SendFleetRPC(ctx context.Context, nodeID string, payload []byte) ([]byte, error) {
	if s == nil || !s.IsCenter() || len(payload) == 0 || len(payload) > maxRPCBytes {
		return nil, ErrOffline
	}
	if s.validateNode != nil {
		if err := s.validateNode(ctx, strings.TrimSpace(nodeID)); err != nil {
			s.DisconnectNode(nodeID)
			return nil, ErrUnauthorized
		}
	}
	s.hubMu.RLock()
	session := s.sessions[strings.TrimSpace(nodeID)]
	s.hubMu.RUnlock()
	if session == nil || session.closed.Load() || session.mux.IsClosed() {
		return nil, ErrOffline
	}
	if !reserveStream(&session.streams) {
		return nil, ErrBusy
	}
	defer session.streams.Add(-1)
	stream, err := openStream(ctx, session.mux)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	setConnDeadlineFromContext(stream, ctx)
	stopCancellation := context.AfterFunc(ctx, func() { _ = stream.SetDeadline(time.Now()) })
	defer stopCancellation()
	if err = writeStreamLane(stream, laneFleetRPC); err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	if err = writeFrame(stream, payload); err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	raw, err := readFrame(stream)
	if err != nil {
		return nil, rpcTransportError(ctx, err)
	}
	var response rpcResponse
	if json.Unmarshal(raw, &response) != nil {
		return nil, ErrOffline
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "Fleet tunnel request rejected"
		}
		return nil, errors.New(response.Error)
	}
	session.lastSeenAt.Store(time.Now().UTC().UnixMilli())
	return append([]byte(nil), response.Payload...), nil
}

func (s *Service) DisconnectNode(nodeID string) {
	if s == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	s.hubMu.Lock()
	session := s.sessions[nodeID]
	delete(s.sessions, nodeID)
	s.hubMu.Unlock()
	if session != nil {
		session.close()
	}
}

func (s *Service) ConfigureClient(config ClientConfig) {
	if s == nil {
		return
	}
	s.StopClient()
	if s.IsCenter() || strings.TrimSpace(config.CenterBaseURL) == "" ||
		strings.TrimSpace(config.Identity.Username) == "" || strings.TrimSpace(config.Identity.Password) == "" ||
		strings.TrimSpace(config.Identity.ClientID) == "" || strings.TrimSpace(config.Identity.NodeID) == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.clientMu.Lock()
	s.clientCancel = cancel
	s.clientDone = done
	s.clientMu.Unlock()
	go s.runClient(ctx, done, config)
}

func (s *Service) StopClient() {
	if s == nil {
		return
	}
	s.clientMu.Lock()
	cancel := s.clientCancel
	done := s.clientDone
	s.clientCancel = nil
	s.clientDone = nil
	s.clientMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.closed) })
	s.StopClient()
	s.hubMu.Lock()
	sessions := make([]*hubSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = make(map[string]*hubSession)
	s.hubMu.Unlock()
	for _, session := range sessions {
		session.close()
	}
}

func (s *Service) runClient(ctx context.Context, done chan struct{}, config ClientConfig) {
	defer close(done)
	delay := s.reconnectMin
	preferredEndpoint := ""
	for {
		if ctx.Err() != nil {
			return
		}
		connectedEndpoint, err := s.connectOnce(ctx, config, preferredEndpoint)
		if connectedEndpoint != "" {
			preferredEndpoint = connectedEndpoint
		}
		if ctx.Err() != nil {
			return
		}
		logx.Errorf("fleet tunnel connection ended fleetNodeID=%s: %v", config.Identity.NodeID, err)
		wait := delay + time.Duration(rand.Int64N(max(int64(delay/2), 1)))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.closed:
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < s.reconnectMax {
			delay *= 2
			if delay > s.reconnectMax {
				delay = s.reconnectMax
			}
		}
	}
}

func (s *Service) connectOnce(ctx context.Context, config ClientConfig, preferredEndpoint string) (string, error) {
	endpoints, err := tunnelEndpointCandidates(config.CenterBaseURL)
	if err != nil {
		return "", err
	}
	for index, endpoint := range endpoints {
		if endpoint == preferredEndpoint && index > 0 {
			endpoints[0], endpoints[index] = endpoints[index], endpoints[0]
			break
		}
	}
	connectErrors := make([]error, 0, len(endpoints))
	for index, endpoint := range endpoints {
		established, connectErr := s.connectEndpoint(ctx, config.Identity, endpoint)
		if established {
			return endpoint, connectErr
		}
		connectErrors = append(connectErrors, fmt.Errorf("%s: %w", endpoint, connectErr))
		if index+1 >= len(endpoints) || !shouldTryNextTunnelEndpoint(ctx, connectErr) {
			break
		}
		logx.Infof("fleet tunnel endpoint unavailable; trying compatibility candidate fleetNodeID=%s current=%s next=%s", config.Identity.NodeID, endpoint, endpoints[index+1])
	}
	return "", errors.Join(connectErrors...)
}

type tunnelDialError struct {
	statusCode int
	err        error
}

func (e *tunnelDialError) Error() string {
	if e == nil || e.err == nil {
		return "fleet tunnel dial failed"
	}
	return e.err.Error()
}

func (e *tunnelDialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func shouldTryNextTunnelEndpoint(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var dialErr *tunnelDialError
	if errors.As(err, &dialErr) {
		return dialErr.statusCode != http.StatusUnauthorized && dialErr.statusCode != http.StatusForbidden
	}
	return true
}

func (s *Service) connectEndpoint(ctx context.Context, identity Identity, endpoint string) (bool, error) {
	headers := http.Header{}
	headers.Set(clientIDHeader, identity.ClientID)
	headers.Set(nodeIDHeader, identity.NodeID)
	headers.Set("Authorization", basicAuthorization(identity.Username, identity.Password))
	dialer := *s.websocketDialer
	dialer.Subprotocols = []string{Subprotocol}
	ws, response, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return false, &tunnelDialError{statusCode: statusCode, err: err}
	}
	if ws.Subprotocol() != Subprotocol {
		_ = ws.Close()
		return false, fmt.Errorf("fleet tunnel protocol negotiation failed")
	}
	transport := newWebsocketConn(ws, nil)
	mux, err := yamux.Client(transport, yamuxConfig())
	if err != nil {
		_ = transport.Close()
		return false, err
	}
	defer mux.Close()
	defer transport.Close()
	clientSession := &outboundSession{mux: mux, transport: transport, nodeID: identity.NodeID}
	s.setClientSession(clientSession)
	defer s.clearClientSession(clientSession)
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			_ = mux.Close()
			_ = transport.Close()
		case <-s.closed:
			_ = mux.Close()
			_ = transport.Close()
		case <-stopWatcher:
		}
	}()
	logx.Infof("fleet tunnel established fleetNodeID=%s protocol=%s endpoint=%s", identity.NodeID, ProtocolVersion, endpoint)

	for {
		stream, acceptErr := mux.AcceptStream()
		if acceptErr != nil {
			return true, acceptErr
		}
		if !reserveStream(&clientSession.streams) {
			_ = stream.Close()
			continue
		}
		go func() {
			defer clientSession.streams.Add(-1)
			s.serveEdgeStream(ctx, stream)
		}()
	}
}

func (s *Service) setClientSession(session *outboundSession) {
	s.clientSessionMu.Lock()
	previous := s.clientSession
	s.clientSession = session
	s.clientSessionMu.Unlock()
	if previous != nil && previous != session {
		previous.close()
	}
}

func (s *Service) clearClientSession(session *outboundSession) {
	s.clientSessionMu.Lock()
	if s.clientSession == session {
		s.clientSession = nil
	}
	s.clientSessionMu.Unlock()
	session.close()
}

func (s *Service) serveEdgeStream(ctx context.Context, stream net.Conn) {
	defer stream.Close()
	lane, err := readStreamLane(stream)
	if err != nil {
		return
	}
	switch lane {
	case laneProxy:
		s.relayToLocalBackend(ctx, stream)
	case laneFleetRPC:
		s.serveFleetRPC(ctx, stream)
	}
}

func (s *Service) serveFleetRPC(ctx context.Context, stream net.Conn) {
	request, err := readFrame(stream)
	if err != nil {
		return
	}
	s.handlerMu.RLock()
	handler := s.fleetRPCHandler
	s.handlerMu.RUnlock()
	response := rpcResponse{}
	if handler == nil {
		response.Error = "Fleet tunnel handler unavailable"
	} else if result, handleErr := handler(ctx, request); handleErr != nil {
		response.Error = handleErr.Error()
	} else {
		response.OK = true
		response.Payload = append(json.RawMessage(nil), result...)
	}
	if raw, marshalErr := json.Marshal(response); marshalErr == nil {
		_ = writeFrame(stream, raw)
	}
}

func (s *Service) acceptCenterStreams(ctx context.Context, session *hubSession) {
	for {
		if !s.IsCenter() {
			session.close()
			return
		}
		stream, err := session.mux.AcceptStream()
		if err != nil {
			return
		}
		if !reserveStream(&session.streams) {
			_ = stream.Close()
			continue
		}
		go func() {
			defer session.streams.Add(-1)
			s.serveCenterStream(ctx, session, stream)
		}()
	}
}

func (s *Service) serveCenterStream(ctx context.Context, session *hubSession, stream net.Conn) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(30 * time.Second))
	if !s.IsCenter() {
		return
	}
	lane, err := readStreamLane(stream)
	if err != nil || lane != laneUNSSync {
		return
	}
	request, err := readFrameBounded(stream, maxUNSSyncBytes)
	if err != nil {
		return
	}
	s.handlerMu.RLock()
	handler := s.unsSyncHandler
	s.handlerMu.RUnlock()
	response := rpcResponse{}
	if handler == nil {
		response.Error = "UNS tunnel handler unavailable"
	} else {
		result, handleErr := handler(ctx, request)
		if handleErr != nil {
			response.Error = handleErr.Error()
		} else {
			response.OK = true
			response.Payload = append(json.RawMessage(nil), result...)
		}
	}
	raw, marshalErr := json.Marshal(response)
	if marshalErr == nil {
		_ = writeFrameBounded(stream, raw, maxUNSSyncBytes)
	}
	if response.OK {
		session.lastSeenAt.Store(time.Now().UTC().UnixMilli())
	}
}

func (s *Service) relayToLocalBackend(ctx context.Context, stream net.Conn) {
	local, err := s.localDialContext(ctx, "tcp", s.localBackendAddr)
	if err != nil {
		_ = stream.Close()
		return
	}
	defer stream.Close()
	defer local.Close()
	done := make(chan struct{}, 2)
	copyHalf := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if closer, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyHalf(local, stream)
	go copyHalf(stream, local)
	select {
	case <-done:
	case <-ctx.Done():
	case <-s.closed:
	}
}

func (s *Service) register(session *hubSession) {
	s.hubMu.Lock()
	previous := s.sessions[session.nodeID]
	s.sessions[session.nodeID] = session
	s.hubMu.Unlock()
	if previous != nil && previous != session {
		previous.close()
	}
}

func (s *Service) unregister(session *hubSession) {
	s.hubMu.Lock()
	if s.sessions[session.nodeID] == session {
		delete(s.sessions, session.nodeID)
	}
	s.hubMu.Unlock()
	session.close()
}

func (s *hubSession) close() {
	if s != nil && s.closed.CompareAndSwap(false, true) {
		_ = s.mux.Close()
		_ = s.transport.Close()
	}
}

func (s *outboundSession) close() {
	if s != nil && s.closed.CompareAndSwap(false, true) {
		_ = s.mux.Close()
		_ = s.transport.Close()
	}
}

func reserveStream(streams *atomic.Int32) bool {
	for {
		current := streams.Load()
		if current >= maxStreams {
			return false
		}
		if streams.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func openStream(ctx context.Context, session *yamux.Session) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		stream, err := session.OpenStream()
		resultCh <- result{conn: stream, err: err}
	}()
	select {
	case opened := <-resultCh:
		if opened.err != nil {
			return nil, ErrOffline
		}
		return opened.conn, nil
	case <-ctx.Done():
		go func() {
			opened := <-resultCh
			if opened.conn != nil {
				_ = opened.conn.Close()
			}
		}()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
}

func writeStreamLane(conn net.Conn, lane string) error {
	header := make([]byte, streamHeaderSize)
	copy(header, streamHeader+lane)
	return writeAll(conn, header)
}

func readStreamLane(conn net.Conn) (string, error) {
	header := make([]byte, streamHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	value := strings.TrimRight(string(header), "\x00")
	if !strings.HasPrefix(value, streamHeader) {
		return "", ErrUnauthorized
	}
	return strings.TrimPrefix(value, streamHeader), nil
}

func writeFrame(conn net.Conn, payload []byte) error {
	return writeFrameBounded(conn, payload, maxRPCBytes)
}

func writeFrameBounded(conn net.Conn, payload []byte, limit int) error {
	if len(payload) == 0 || len(payload) > limit {
		return ErrBusy
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if err := writeAll(conn, header); err != nil {
		return err
	}
	return writeAll(conn, payload)
}

func writeAll(conn net.Conn, payload []byte) error {
	for len(payload) > 0 {
		written, err := conn.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrUnexpectedEOF
		}
		payload = payload[written:]
	}
	return nil
}

func readFrame(conn net.Conn) ([]byte, error) {
	return readFrameBounded(conn, maxRPCBytes)
}

func readFrameBounded(conn net.Conn, limit int) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header))
	if size <= 0 || size > limit {
		return nil, ErrBusy
	}
	payload := make([]byte, size)
	_, err := io.ReadFull(conn, payload)
	return payload, err
}

func setConnDeadlineFromContext(conn net.Conn, ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
}

// RPC 取消只结束本次逻辑流；已发出的业务操作不因此撤销或自动重试。
func rpcTransportError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return ErrTimeout
	}
	return ErrOffline
}

func yamuxConfig() *yamux.Config {
	config := yamux.DefaultConfig()
	config.AcceptBacklog = int(maxStreams)
	config.EnableKeepAlive = true
	config.KeepAliveInterval = 20 * time.Second
	// A peer must answer the first keepalive within 40 seconds. Together with
	// the 20-second interval this implements the documented 60-second liveness
	// boundary without a second heartbeat protocol beside yamux.
	config.ConnectionWriteTimeout = 40 * time.Second
	config.StreamOpenTimeout = 10 * time.Second
	config.LogOutput = io.Discard
	return config
}

func tunnelEndpoint(centerBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(centerBaseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid fleet tunnel center URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
		parsed.Scheme = strings.ToLower(parsed.Scheme)
	default:
		return "", fmt.Errorf("unsupported fleet tunnel center URL scheme")
	}
	parsed.Path = path.Join(parsed.Path, ConnectPath)
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func tunnelEndpointCandidates(centerBaseURL string) ([]string, error) {
	primary, err := tunnelEndpoint(centerBaseURL)
	if err != nil {
		return nil, err
	}
	endpoints := []string{primary}
	if fallback := legacySecureTunnelEndpoint(centerBaseURL); fallback != "" && fallback != primary {
		endpoints = append(endpoints, fallback)
	}
	return endpoints, nil
}

// legacySecureTunnelEndpoint supports tokens issued before the tunnel existed.
// Those tokens could contain the install-time HTTP listener on :8088 even when
// users reached the center through a public HTTPS domain. The original address
// remains first; only an unavailable legacy domain endpoint is upgraded to the
// same host's default WSS entry. IP/localhost and custom ports are left alone.
func legacySecureTunnelEndpoint(centerBaseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(centerBaseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "ws") || parsed.Port() != "8088" {
		return ""
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" || strings.EqualFold(hostname, "localhost") || net.ParseIP(hostname) != nil {
		return ""
	}
	parsed.Scheme = "wss"
	parsed.Host = hostname
	parsed.Path = path.Join(parsed.Path, ConnectPath)
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func containsSubprotocol(protocols []string, expected string) bool {
	for _, protocol := range protocols {
		if strings.TrimSpace(protocol) == expected {
			return true
		}
	}
	return false
}

func basicAuthorization(username, password string) string {
	req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	req.SetBasicAuth(username, password)
	return req.Header.Get("Authorization")
}

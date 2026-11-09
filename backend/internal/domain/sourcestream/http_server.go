package sourcestream

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/google/uuid"
)

type httpRoute struct {
	stream  repo.Stream
	version repo.StreamVersion
	entry   streamexec.ProgramEntry
}
type httpSourceSnapshot struct {
	source     repo.Source
	definition SourceDefinition
	credential Credential
	routes     []httpRoute
	renewed    time.Time
}
type httpSourceListener struct {
	server   *http.Server
	port     int
	snapshot httpSourceSnapshot
}
type HTTPServerManager struct {
	mu          sync.RWMutex
	listeners   map[int64]*httpSourceListener
	errors      map[int64]string
	admission   chan struct{}
	connections chan struct{}
	stop        context.CancelFunc
}

func NewHTTPServerManager() *HTTPServerManager {
	return &HTTPServerManager{listeners: map[int64]*httpSourceListener{}, errors: map[int64]string{}, admission: make(chan struct{}, 4), connections: make(chan struct{}, 64)}
}

func HTTPServerPortRange() (int, int, error) {
	read := func(key string, fallback int) (int, error) {
		if value := os.Getenv(key); value != "" {
			return strconv.Atoi(value)
		}
		return fallback, nil
	}
	first, err := read("SOURCE_HTTP_PORT_RANGE_START", 31000)
	if err != nil {
		return 0, 0, ErrUnavailable
	}
	last, err := read("SOURCE_HTTP_PORT_RANGE_END", 31015)
	if err != nil || first < 1024 || last < first || last > 65535 || last-first >= 256 {
		return 0, 0, ErrUnavailable
	}
	appFirst, e1 := read("APP_GATEWAY_PORT_RANGE_START", 30000)
	appLast, e2 := read("APP_GATEWAY_PORT_RANGE_END", 30099)
	if e1 != nil || e2 != nil || first <= appLast && last >= appFirst {
		return 0, 0, ErrUnavailable
	}
	appHostFirst, e1 := read("APP_GATEWAY_HOST_PORT_RANGE_START", 30100)
	appHostLast, e2 := read("APP_GATEWAY_HOST_PORT_RANGE_END", 30199)
	if e1 != nil || e2 != nil || first <= appHostLast && last >= appHostFirst {
		return 0, 0, ErrUnavailable
	}
	return first, last, nil
}

func httpSourcePublicURL(port int) string {
	host := os.Getenv("SOURCE_HTTP_PUBLIC_HOST")
	if host == "" {
		host = os.Getenv("ENTRANCE_DOMAIN")
	}
	if host == "" {
		host = os.Getenv("APP_GATEWAY_EXTERNAL_HOST")
	}
	if host == "" || strings.ContainsAny(host, "/?#@\\\r\n") {
		return ""
	}
	if strings.Contains(host, ":") && net.ParseIP(strings.Trim(host, "[]")) == nil {
		return ""
	}
	scheme := os.Getenv("SOURCE_HTTP_PUBLIC_SCHEME")
	if scheme == "" {
		scheme = os.Getenv("ENTRANCE_PROTOCOL")
	}
	if scheme == "" {
		scheme = os.Getenv("APP_GATEWAY_SCHEME")
	}
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))}).String()
}

func (m *HTTPServerManager) View(id int64, port int) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, message := "stopped", ""
	if entry := m.listeners[id]; entry != nil && time.Since(entry.snapshot.renewed) < 15*time.Second {
		state = "listening"
	}
	if failure := m.errors[id]; failure != "" {
		state, message = "failed", failure
	}
	return map[string]any{"port": port, "state": state, "message": message, "url": httpSourcePublicURL(port)}
}

func (m *HTTPServerManager) CloseSource(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if listener := m.listeners[id]; listener != nil {
		_ = listener.server.Close()
		delete(m.listeners, id)
	}
	delete(m.errors, id)
}
func (m *HTTPServerManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	for id, listener := range m.listeners {
		_ = listener.server.Close()
		delete(m.listeners, id)
	}
}

// A lease expires closed if a full DB/route snapshot cannot be renewed.
func (m *HTTPServerManager) Sync(service *Service, sources []httpSourceSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := map[int64]bool{}
	var firstErr error
	for _, snapshot := range sources {
		id, port := snapshot.source.ID, snapshot.definition.HTTP.Port
		keep[id] = true
		if previous := m.listeners[id]; previous != nil && previous.port != port {
			_ = previous.server.Close()
			delete(m.listeners, id)
		}
		snapshot.renewed = time.Now()
		if previous := m.listeners[id]; previous != nil {
			previous.snapshot = snapshot
			continue
		}
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			m.errors[id] = "SourceStream.httpPortInUse"
			if firstErr == nil {
				firstErr = ErrUnavailable
			}
			continue
		}
		server := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: streamexec.MaxExecutorTransportTimeout, WriteTimeout: streamexec.MaxExecutorTransportTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
		server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.serve(service, id, w, r) })
		m.listeners[id] = &httpSourceListener{server: server, port: port, snapshot: snapshot}
		delete(m.errors, id)
		go func() { _ = server.Serve(&httpBoundedListener{Listener: listener, slots: m.connections}) }()
	}
	for id, listener := range m.listeners {
		if !keep[id] {
			_ = listener.server.Close()
			delete(m.listeners, id)
			delete(m.errors, id)
		}
	}
	return firstErr
}

func httpRouteMatch(pattern, path string) (map[string]string, bool) {
	if strings.ContainsAny(path, "%\\") {
		return nil, false
	}
	parts, values := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(parts) != len(values) {
		return nil, false
	}
	params := map[string]string{}
	for i, part := range parts {
		if values[i] == "." || values[i] == ".." {
			return nil, false
		}
		if strings.HasPrefix(part, ":") {
			if values[i] == "" {
				return nil, false
			}
			params[strings.TrimPrefix(part, ":")] = values[i]
		} else if part != values[i] {
			return nil, false
		}
	}
	return params, true
}

func (m *HTTPServerManager) serve(service *Service, id int64, w http.ResponseWriter, r *http.Request) {
	writeError := func(status int) { http.Error(w, http.StatusText(status), status) }
	m.mu.RLock()
	listener := m.listeners[id]
	if listener == nil {
		m.mu.RUnlock()
		writeError(http.StatusServiceUnavailable)
		return
	}
	snapshot := listener.snapshot
	m.mu.RUnlock()
	if time.Since(snapshot.renewed) >= 15*time.Second {
		writeError(http.StatusServiceUnavailable)
		return
	}
	if !snapshot.definition.HTTP.Anonymous {
		token := snapshot.credential.Secret
		if snapshot.credential.Values["token"] != "" {
			token = snapshot.credential.Values["token"]
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
			writeError(http.StatusUnauthorized)
			return
		}
	}
	var route *httpRoute
	var params map[string]string
	for i := range snapshot.routes {
		candidate := &snapshot.routes[i]
		if candidate.entry.Method != r.Method {
			continue
		}
		if found, matches := httpRouteMatch(candidate.entry.Path, r.URL.EscapedPath()); matches {
			route, params = candidate, found
			break
		}
	}
	if route == nil {
		writeError(http.StatusNotFound)
		return
	}
	select {
	case m.admission <- struct{}{}:
		defer func() { <-m.admission }()
	default:
		writeError(http.StatusTooManyRequests)
		return
	}
	if r.ContentLength > 1024*1024 || len(r.URL.RawQuery) > 16*1024 {
		writeError(http.StatusRequestEntityTooLarge)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if err != nil {
		writeError(http.StatusRequestEntityTooLarge)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(http.StatusBadRequest)
		return
	}
	var body any
	if len(raw) > 0 {
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			if json.Unmarshal(raw, &body) != nil {
				writeError(http.StatusBadRequest)
				return
			}
		} else {
			body = string(raw)
		}
	}
	headers := r.Header.Clone()
	for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		headers.Del(name)
	}
	requestID := uuid.NewString()
	input := map[string]any{"method": r.Method, "path": r.URL.Path, "params": params, "query": query, "headers": headers, "body": body, "requestId": requestID}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 1024*1024 {
		writeError(http.StatusRequestEntityTooLarge)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), streamexec.MaxExecutorTransportTimeout)
	defer cancel()
	result, err := service.executeHTTPRoute(ctx, snapshot, *route, requestID, input)
	if err != nil {
		writeError(httpRouteErrorStatus(ctx, err))
		return
	}
	w.Header().Set("X-Request-ID", requestID)
	if err := writeHTTPRouteResult(w, result); err != nil {
		writeError(http.StatusBadGateway)
	}
}

func httpRouteErrorStatus(ctx context.Context, err error) int {
	// A cancelled host call or audit finalization must not disguise the deadline.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, streamexec.ErrTimeout) {
		return http.StatusGatewayTimeout
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, repo.ErrConflict) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

func writeHTTPRouteResult(w http.ResponseWriter, raw json.RawMessage) error {
	var response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status > 599 || len(response.Headers) > 32 {
		return repo.ErrInvalidArgument
	}
	var body []byte
	var text string
	if len(response.Body) > 0 {
		if json.Unmarshal(response.Body, &text) == nil {
			body = []byte(text)
		} else {
			body = response.Body
		}
	}
	if len(body) > 1024*1024 {
		return repo.ErrInvalidArgument
	}
	for key, value := range response.Headers {
		if !validHTTPResponseHeader(key) || len(key) > 128 || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
			return repo.ErrInvalidArgument
		}
		switch strings.ToLower(key) {
		case "connection", "transfer-encoding", "content-length", "upgrade", "proxy-authenticate", "proxy-authorization", "trailer", "keep-alive":
			return repo.ErrInvalidArgument
		}
	}
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(response.Status)
	if response.Status != http.StatusNoContent && response.Status != http.StatusNotModified {
		_, _ = w.Write(body)
	}
	return nil
}

func validHTTPResponseHeader(key string) bool {
	if key == "" {
		return false
	}
	for _, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}

func (s *Service) executeHTTPRoute(ctx context.Context, snapshot httpSourceSnapshot, route httpRoute, requestID string, input any) (json.RawMessage, error) {
	ctx, _ = WithProgramSecretGuard(ctx)
	recordHTTPCredential(ctx, snapshot.credential)
	ref := ExecutionRef{SourceID: snapshot.source.ID, SourceRevision: snapshot.source.ConfigRevision, StreamID: route.stream.ID, VersionID: route.version.ID, EntryID: route.entry.ID}
	task, _, err := s.prepareProgramV2Task(ctx, requestID, ref, input, time.Now().UnixMilli(), "http-request", 0, true, nil)
	if err != nil {
		return nil, err
	}
	result, err := s.commitProgramV2Task(ctx, task, nil, s.programSink, false)
	return result.Data, err
}

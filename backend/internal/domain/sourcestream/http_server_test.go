package sourcestream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestHTTPRouteTimeoutStatusSurvivesIPCAndFinalization(t *testing.T) {
	live := context.Background()
	require.Equal(t, http.StatusGatewayTimeout, httpRouteErrorStatus(live, streamexec.ErrTimeout))
	require.Equal(t, http.StatusGatewayTimeout, httpRouteErrorStatus(live, errors.Join(streamexec.ErrTimeout, repo.ErrConflict)))
	expired, cancel := context.WithDeadline(live, time.Now().Add(-time.Second))
	defer cancel()
	// Generic host-call failure cannot mask the authoritative request deadline.
	require.Equal(t, http.StatusGatewayTimeout, httpRouteErrorStatus(expired, streamexec.ErrSourceCall))
	require.Equal(t, http.StatusGatewayTimeout, httpRouteErrorStatus(expired, repo.ErrConflict))
	require.Equal(t, http.StatusServiceUnavailable, httpRouteErrorStatus(live, repo.ErrConflict))
	require.Equal(t, http.StatusServiceUnavailable, httpRouteErrorStatus(live, ErrUnavailable))
	require.Equal(t, http.StatusBadGateway, httpRouteErrorStatus(live, ErrHTTPRequest))
	require.Equal(t, http.StatusBadGateway, httpRouteErrorStatus(live, streamexec.ErrExecution))
}

func TestHTTPServerRoutingAndResponseBoundaries(t *testing.T) {
	params, ok := httpRouteMatch("/orders/:id", "/orders/42")
	require.True(t, ok)
	require.Equal(t, "42", params["id"])
	for _, path := range []string{"/orders/", "/orders/../x", "/orders/%2e%2e", "/orders/a/b", "/orders/a\\b"} {
		_, ok = httpRouteMatch("/orders/:id", path)
		require.False(t, ok, path)
	}
	w := httptest.NewRecorder()
	require.NoError(t, writeHTTPRouteResult(w, json.RawMessage(`{"status":201,"headers":{"X-Result":"ok"},"body":{"id":42}}`)))
	require.Equal(t, 201, w.Code)
	require.JSONEq(t, `{"id":42}`, w.Body.String())
	for _, value := range []string{
		`{"status":101}`, `{"status":200,"headers":{"Bad Header":"value"}}`,
		`{"status":200,"headers":{"Content-Length":"5"}}`, `{"status":200,"headers":{"X-Test":"bad\r\nvalue"}}`,
		`{"status":200,"headers":{"X-Test":"bad\u0000value"}}`,
	} {
		require.Error(t, writeHTTPRouteResult(httptest.NewRecorder(), json.RawMessage(value)), value)
	}
	raw, err := json.Marshal(map[string]any{"status": 200, "body": strings.Repeat("x", 1024*1024+1)})
	require.NoError(t, err)
	require.Error(t, writeHTTPRouteResult(httptest.NewRecorder(), raw))
}

func TestHTTPServerRejectsBeforeExecutingAndExpiresClosed(t *testing.T) {
	m := NewHTTPServerManager()
	t.Cleanup(m.Close)
	m.listeners[1] = &httpSourceListener{snapshot: httpSourceSnapshot{
		source: repo.Source{ID: 1}, definition: SourceDefinition{HTTP: &HTTPConnection{}},
		credential: Credential{Secret: "fixture-bearer"}, renewed: time.Now(),
		routes: []httpRoute{{entry: streamexec.ProgramEntry{Method: "POST", Path: "/orders/:id"}}},
	}}
	// No Service is supplied: every branch below must reject before executing.
	request := func(body, path, token string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", token)
		r.Header.Set("Content-Type", "application/json")
		m.serve(nil, 1, w, r)
		return w.Code
	}
	require.Equal(t, 401, request(`{}`, "/orders/1", ""))
	require.Equal(t, 401, request(`{}`, "/orders/1", "Bearer wrong"))
	require.Equal(t, 404, request(`{}`, "/missing", "Bearer fixture-bearer"))
	require.Equal(t, 400, request(`{`, "/orders/1", "Bearer fixture-bearer"))
	require.Equal(t, 400, request(`{}`, "/orders/1?q=%ff%", "Bearer fixture-bearer"))
	require.Equal(t, 413, request(strings.Repeat("x", 1024*1024+1), "/orders/1", "Bearer fixture-bearer"))
	for i := 0; i < cap(m.admission); i++ {
		m.admission <- struct{}{}
	}
	require.Equal(t, 429, request(`{}`, "/orders/1", "Bearer fixture-bearer"))
	for i := 0; i < cap(m.admission); i++ {
		<-m.admission
	}
	entry := m.listeners[1]
	entry.snapshot.renewed = time.Now().Add(-16 * time.Second)
	require.Equal(t, 503, request(`{}`, "/orders/1", "Bearer fixture-bearer"))
	delete(m.listeners, 1)
}

func TestHTTPSourcePublicURLAndPortConfiguration(t *testing.T) {
	t.Setenv("SOURCE_HTTP_PUBLIC_HOST", "enterprise.example.test")
	t.Setenv("SOURCE_HTTP_PUBLIC_SCHEME", "https")
	require.Equal(t, "https://enterprise.example.test:31000", httpSourcePublicURL(31000))
	t.Setenv("SOURCE_HTTP_PUBLIC_HOST", "enterprise.example.test:443")
	require.Empty(t, httpSourcePublicURL(31000))
	t.Setenv("SOURCE_HTTP_PORT_RANGE_START", "31000")
	t.Setenv("SOURCE_HTTP_PORT_RANGE_END", "31015")
	first, last, err := HTTPServerPortRange()
	require.NoError(t, err)
	require.Equal(t, 31000, first)
	require.Equal(t, 31015, last)
	for _, start := range []string{"30000", "30100", "65536", "not-a-port"} {
		t.Setenv("SOURCE_HTTP_PORT_RANGE_START", start)
		_, _, err = HTTPServerPortRange()
		require.Error(t, err, start)
	}
}

func TestHTTPServerUnprovisionedSourceDoesNotBlockRenewal(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	t.Setenv("SOURCE_HTTP_ENABLED", "true")
	t.Setenv("SOURCE_HTTP_PORT_RANGE_START", strconv.Itoa(port))
	t.Setenv("SOURCE_HTTP_PORT_RANGE_END", strconv.Itoa(port+1))
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.Close)
	def := SourceDefinition{Name: "healthy", Protocol: HTTP, Role: ServerRole,
		Endpoint: "http://managed", RuntimeLocation: "local", HTTP: &HTTPConnection{Anonymous: true}}
	healthy, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := healthy["id"].(int64)
	require.NoError(t, s.ReconcileHTTPServers(ctx))
	def.Name, def.HTTP = "unprovisioned", &HTTPConnection{}
	_, err = s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	s.httpServers.mu.Lock()
	s.httpServers.listeners[id].snapshot.renewed = time.Now().Add(-16 * time.Second)
	s.httpServers.mu.Unlock()
	request := func() int {
		w := httptest.NewRecorder()
		s.httpServers.serve(s, id, w, httptest.NewRequest(http.MethodGet, "/missing", nil))
		return w.Code
	}
	require.Equal(t, http.StatusServiceUnavailable, request())
	require.NoError(t, s.ReconcileHTTPServers(ctx))
	require.Equal(t, http.StatusNotFound, request(), "healthy listener lease is renewed even when another Source lacks credentials")
	s.httpServers.mu.Lock()
	count := len(s.httpServers.listeners)
	s.httpServers.mu.Unlock()
	require.Equal(t, 1, count, "unprovisioned Source must not receive a listener")
}

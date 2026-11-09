package sourcestream

// 使用真实HTTP测试端与私有测试拨号器验证请求、超时、边界及源级连接池。
// 生产DNS/IP防护仍启用；测试拨号器只把已验证私网目标拨到本测试夹具。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestCloudTimeoutFinalizationSurvivesCancellation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			run := repo.StreamRun{ID: fmt.Sprintf("cloud-timeout-%t", uncertain), StreamID: 101, VersionID: 102, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}
			require.NoError(t, store.UnsDB().Create(&run).Error)
			if uncertain {
				effect := repo.StreamProgramEffect{RunID: run.ID, EntryID: run.EntryID, CallID: "1", OpID: "source.request", RequestHash: strings.Repeat("a", 64), Status: "dispatching", Result: `{}`}
				require.NoError(t, store.UnsDB().Create(&effect).Error)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := (&Service{}).failProgramV2Run(ctx, run, streamexec.ErrTimeout)
			want := "failed"
			if uncertain {
				want = "unknown"
				require.ErrorIs(t, err, ErrUnknownResult)
			} else {
				require.ErrorIs(t, err, ErrExecutionFailed)
				require.NotErrorIs(t, err, ErrUnknownResult)
			}
			r := repo.NewSourceStreamRepo(context.Background())
			stored, err := r.Run(context.Background(), run.ID)
			require.NoError(t, err)
			require.Equal(t, want, stored.Status)
			require.ErrorIs(t, r.FinishRun(context.Background(), run.ID, "succeeded", `{}`, 0), repo.ErrConflict, "late result must not overwrite terminal status")
		})
	}
}

func httpTestManager(t *testing.T, manager *HTTPClientManager, server *httptest.Server) {
	t.Helper()
	manager.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.9.0.1")}, nil
	}
	manager.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "10.9.0.1" {
			return nil, ErrHTTPRequest
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	t.Cleanup(manager.Close)
}

// 登记的只读 POST 分页应复用 Source 连接；普通 POST 仍关闭连接防止隐式重放。
// 使用真实 HTTP/TLS 连接，不以客户端缓存条目数量代替连接复用证据。
func TestHTTPRegisteredReadOnlyPOSTReusesSourceTransport(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, readOnly := range []bool{false, true} {
			name := "http"
			if secure {
				name = "https"
			}
			if readOnly {
				name += "-registered-read"
			} else {
				name += "-ordinary-write"
			}
			t.Run(name, func(t *testing.T) {
				var connections atomic.Int32
				closes := make(chan bool, 2)
				fixture := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					closes <- r.Close
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				fixture.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						connections.Add(1)
					}
				}
				if secure {
					fixture.StartTLS()
				} else {
					fixture.Start()
				}
				defer fixture.Close()
				s := New(nil, nil, "")
				httpTestManager(t, s.httpManager, fixture)
				endpoint := "http://fixture.internal/api"
				if secure {
					endpoint = "https://example.com/api"
				}
				source := repo.Source{ID: 19, ConfigRevision: 1}
				def := SourceDefinition{Protocol: HTTP, Endpoint: endpoint, HTTP: &HTTPConnection{Authentication: "none"}}
				client, err := s.httpManager.client(source, def)
				require.NoError(t, err)
				if secure {
					client.Transport.(*http.Transport).TLSClientConfig = fixture.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
				}
				ctx := context.Background()
				if readOnly {
					ctx = context.WithValue(ctx, vendorReadOnlyContext{}, true)
				}
				for i := 0; i < 2; i++ {
					_, err = s.HTTPProgramCall(ctx, source, def, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"POST","path":"query","json":{"page":1}}`)})
					require.NoError(t, err)
				}
				wantConnections := int32(2)
				if readOnly {
					wantConnections = 1
				}
				require.Equal(t, wantConnections, connections.Load())
				require.Equal(t, !readOnly, <-closes)
				require.Equal(t, !readOnly, <-closes)
			})
		}
	}
}

// 业务层未登录可以是HTTP200；只读原生操作最多恢复一次，普通POST不能重放。
func TestHTTPTeamcenterSessionRecoveryIsReadOnlyAndBounded(t *testing.T) {
	for _, example := range []struct {
		name, qname, businessError string
		status, reads, logins      int
		readOnly, alwaysRejected   bool
	}{
		{"native-read-expired-once", "InvalidUserException", "", 200, 2, 2, true, false},
		{"native-read-always-expired", "InvalidUserException", "vendorSessionExpired", 200, 2, 2, true, true},
		{"native-read-http-unauthorized", "InvalidUserException", "", 401, 2, 2, true, false},
		{"generic-post-never-replayed", "InvalidUserException", "vendorSessionExpired", 200, 1, 1, false, false},
		{"other-exception-never-replayed", "InternalServerException", "vendorException", 200, 1, 1, true, true},
	} {
		t.Run(example.name, func(t *testing.T) {
			var reads, logins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/login") {
					generation := logins.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "session", Value: fmt.Sprint(generation), Path: "/", HttpOnly: true})
					_, _ = w.Write([]byte(`{"ServiceData":{"partialErrors":[]}}`))
					return
				}
				attempt := reads.Add(1)
				require.Equal(t, http.MethodPost, r.Method)
				cookie, cookieErr := r.Cookie("session")
				if cookieErr != nil || cookie.Value != fmt.Sprint(logins.Load()) {
					_, _ = w.Write([]byte(`{".QName":"http://teamcenter.com/Schemas/Soa/2006-03/Exceptions.InvalidUserException"}`))
					return
				}
				if attempt == 1 || example.alwaysRejected {
					w.WriteHeader(example.status)
					_ = json.NewEncoder(w).Encode(map[string]any{".QName": "http://teamcenter.com/Schemas/Soa/2006-03/Exceptions." + example.qname, "code": 214001})
					return
				}
				_, _ = w.Write([]byte(`{"items":[{"uid":"entity-1"}]}`))
			}))
			defer server.Close()
			s := New(nil, nil, "")
			httpTestManager(t, s.httpManager, server)
			d := SourceDefinition{Protocol: HTTP, SourceType: "teamcenter", Endpoint: "http://fixture.internal/tc", HTTP: &HTTPConnection{Authentication: "none"}}
			row := repo.Source{ID: 1, ConfigRevision: 1}
			var raw json.RawMessage
			var err error
			if example.readOnly {
				raw, err = s.vendorNativeCall(context.Background(), row, d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:teamcenter:query", Options: json.RawMessage(`{"data":{"body":{}}}`)}, "", "")
			} else {
				raw, err = s.HTTPProgramCall(context.Background(), row, d, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"POST","path":"arbitrary-operation","json":{}}`)})
			}
			require.NoError(t, err)
			var result HTTPResponse
			require.NoError(t, json.Unmarshal(raw, &result))
			require.Equal(t, example.businessError, vendorBusinessError("teamcenter", result.Data))
			require.EqualValues(t, example.reads, reads.Load())
			require.EqualValues(t, example.logins, logins.Load())
			if example.businessError == "" {
				require.Equal(t, 200, result.Status)
				require.Contains(t, string(raw), "entity-1")
			} else if example.readOnly {
				require.Contains(t, string(raw), example.businessError, "terminal native errors must remain visible to scripts")
			}
		})
	}
}

// K3会话失效也可能是HTTP200二维数组；只读查询最多重登一次，写入绝不重放。
func TestHTTPK3SessionRecoveryIsReadOnlyAndBounded(t *testing.T) {
	for _, example := range []struct {
		name, rejected, businessError string
		status, reads, logins         int
		readOnly, alwaysRejected      bool
	}{
		{"query-expired-once", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`, "", 200, 2, 2, true, false},
		{"query-expired-bounded", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`, "vendorSessionExpired", 200, 2, 2, true, true},
		{"query-http-unauthorized", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`, "", 401, 2, 2, true, false},
		{"field-error-not-replayed", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"ErrorCode":"FIELD","MsgCode":0}}}]]`, "vendorBusinessRejected", 200, 1, 1, true, true},
		{"generic-post-not-replayed", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`, "vendorSessionExpired", 200, 1, 1, false, false},
	} {
		t.Run(example.name, func(t *testing.T) {
			var reads, logins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, ".AuthService.") {
					generation := logins.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "kdservice-sessionid", Value: fmt.Sprint(generation), Path: "/", HttpOnly: true})
					_, _ = w.Write([]byte(`{"LoginResultType":1}`))
					return
				}
				cookie, cookieErr := r.Cookie("kdservice-sessionid")
				require.NoError(t, cookieErr, "the Source owns and supplies the session")
				require.Equal(t, http.MethodPost, r.Method)
				attempt := reads.Add(1)
				if cookieErr == nil && cookie.Value != fmt.Sprint(logins.Load()) {
					_, _ = w.Write([]byte(`[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`))
					return
				}
				if attempt == 1 || example.alwaysRejected {
					w.WriteHeader(example.status)
					_, _ = w.Write([]byte(example.rejected))
					return
				}
				_, _ = w.Write([]byte(`[[10000001,"M1","Material"]]`))
			}))
			defer server.Close()
			s := New(nil, nil, "")
			httpTestManager(t, s.httpManager, server)
			d := SourceDefinition{Protocol: HTTP, SourceType: "kingdee-k3cloud", Endpoint: "http://fixture.internal/K3Cloud/", HTTP: &HTTPConnection{Authentication: "none", Parameters: map[string]any{"lcid": "2052", "acctId": "test-only", "user": "reader"}}}
			row := repo.Source{ID: 1, ConfigRevision: 1}
			var raw json.RawMessage
			var err error
			if example.readOnly {
				raw, err = s.vendorNativeCall(context.Background(), row, d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:kingdee-k3cloud:query", Options: json.RawMessage(`{"data":{"data":{"FormId":"BD_MATERIAL","FieldKeys":"FMaterialId,FNumber,FName","Limit":1}}}`)}, "", "")
			} else {
				raw, err = s.HTTPProgramCall(context.Background(), row, d, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"POST","path":"arbitrary-operation","json":{}}`)})
			}
			require.NoError(t, err)
			var result HTTPResponse
			require.NoError(t, json.Unmarshal(raw, &result))
			require.Equal(t, example.businessError, vendorBusinessError("kingdee-k3cloud", result.Data))
			require.EqualValues(t, example.reads, reads.Load())
			require.EqualValues(t, example.logins, logins.Load())
			if example.businessError == "" {
				require.Contains(t, string(raw), "10000001")
			} else if example.readOnly {
				require.Contains(t, string(raw), example.businessError, "terminal rejection must remain visible")
			}
		})
	}
}

func TestHTTPClientRequestRealServerAndLimits(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api/items":
			require.Equal(t, "POST", r.Method)
			require.Equal(t, "2", r.URL.Query().Get("page"))
			require.Equal(t, "signed-value", r.Header.Get("X-Signature"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"id":7}`, string(body))
			w.Header().Set("Set-Cookie", "private=session")
			w.Header().Set("X-Trace", "t1")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"accepted":true}`))
		case "/api/form":
			require.NoError(t, r.ParseForm())
			require.Equal(t, "a b", r.PostForm.Get("key"))
			w.WriteHeader(204)
		case "/api/text":
			_, _ = w.Write([]byte(strings.Repeat("x", 2*maxHTTPBytes)))
		case "/api/redirect":
			w.Header().Set("Location", "http://metadata.google.internal/secret")
			w.WriteHeader(302)
		case "/api/oversize":
			_, _ = w.Write([]byte(strings.Repeat("x", maxHTTPResponseBytes+1)))
		case "/api/slow":
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			_, _ = w.Write([]byte(`{}`))
		case "/api/delayed":
			time.Sleep(5100 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ready":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	s := New(nil, nil, "")
	httpTestManager(t, s.httpManager, server)
	def := SourceDefinition{Name: "HTTP fixture", Protocol: HTTP, Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none", Parameters: map[string]any{"tenant": "a"}}}
	source := repo.Source{ID: 1, ConfigRevision: 1}
	call := streamexec.ProgramCall{ID: 1, Kind: "source.request", SourceID: "1", Options: json.RawMessage(`{"method":"POST","path":"/items","query":{"page":"2"},"headers":{"X-Signature":"signed-value"},"json":{"id":7}}`)}
	raw, err := s.HTTPProgramCall(context.Background(), source, def, call)
	require.NoError(t, err)
	var response HTTPResponse
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, 201, response.Status)
	require.NotContains(t, response.Headers, "Set-Cookie")
	require.Equal(t, []string{"t1"}, response.Headers["X-Trace"])
	for _, entry := range []struct {
		options string
		status  int
		data    any
	}{
		{`{"method":"POST","path":"form","form":{"key":"a b"}}`, 204, nil},
		{`{"path":"text"}`, 200, strings.Repeat("x", 2*maxHTTPBytes)},
		{`{"path":"redirect"}`, 302, nil},
	} {
		call.Options = json.RawMessage(entry.options)
		raw, err = s.HTTPProgramCall(context.Background(), source, def, call)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &response))
		require.Equal(t, entry.status, response.Status)
		require.Equal(t, entry.data, response.Data)
	}
	call.Options = json.RawMessage(`{"path":"oversize"}`)
	_, err = s.HTTPProgramCall(context.Background(), source, def, call)
	require.ErrorIs(t, err, ErrHTTPRequest)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	call.Options = json.RawMessage(`{"path":"slow"}`)
	_, err = s.HTTPProgramCall(ctx, source, def, call)
	require.ErrorIs(t, err, streamexec.ErrTimeout)
	require.Equal(t, int32(6), calls.Load())
	call.Options = json.RawMessage(`{"path":"delayed","timeoutMs":15000}`)
	_, err = s.HTTPProgramCall(context.Background(), source, def, call)
	require.NoError(t, err, "explicit read budget must survive the former 5-second header/client deadline")
	for _, timeout := range []string{"99", "45001", "-1"} {
		_, err = parseHTTPRequest(json.RawMessage(`{"timeoutMs":` + timeout + `}`))
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
	raw, err = s.HTTPProgramCall(context.Background(), source, def, streamexec.ProgramCall{Kind: "source.parameters"})
	require.NoError(t, err)
	require.JSONEq(t, `{"tenant":"a"}`, string(raw))
	raw, err = s.HTTPProgramCall(context.Background(), source, def, streamexec.ProgramCall{Kind: "source.secrets"})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))
}

func TestHTTPFailureRetainsSafeTypedIdentity(t *testing.T) {
	private := "https://private.invalid/?credential=never-expose"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, safeHTTPFailure(ctx, errors.New(private)), context.Canceled)
	timeout := &url.Error{Op: "GET", URL: private, Err: &net.DNSError{Err: private, IsTimeout: true}}
	for _, cause := range []error{timeout, context.DeadlineExceeded, streamexec.ErrTimeout} {
		classified := safeHTTPFailure(context.Background(), cause)
		require.ErrorIs(t, classified, streamexec.ErrTimeout)
		require.NotContains(t, classified.Error(), private)
	}
	require.ErrorIs(t, safeHTTPFailure(context.Background(), errors.New(private)), ErrHTTPRequest)
	require.ErrorIs(t, safeHTTPFailureWithFallback(context.Background(), errors.New(private), ErrOAuth), ErrOAuth)
	require.ErrorIs(t, safeHTTPFailureWithFallback(context.Background(), timeout, ErrOAuth), streamexec.ErrTimeout)
}

func TestHTTPClientTLSHeadersBodyTimeoutAndRecovery(t *testing.T) {
	var requests atomic.Int32
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/headers":
			<-r.Context().Done()
		case "/api/body":
			_, _ = w.Write([]byte(`{"partial":`))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/effect":
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		case "/api/broken-effect":
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer fixture.Close()
	for _, protocol := range []Protocol{HTTP, OAuth} {
		t.Run(string(protocol), func(t *testing.T) {
			s := New(nil, nil, "")
			httpTestManager(t, s.httpManager, fixture)
			source := repo.Source{ID: 1, ConfigRevision: 1}
			def := SourceDefinition{Protocol: protocol, Endpoint: "https://example.com/api", HTTP: &HTTPConnection{Authentication: "none"}}
			client, err := s.httpManager.client(source, def)
			require.NoError(t, err)
			transport := client.Transport.(*http.Transport)
			transport.TLSClientConfig = fixture.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			for _, phase := range []string{"headers", "body", "effect"} {
				before := requests.Load()
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				method := "GET"
				body := ""
				if phase == "effect" {
					method, body = "POST", `,"body":"dispatched-once"`
				}
				call := streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"` + method + `","path":"` + phase + `","headers":{"Authorization":"Bearer private-fixture-only"}` + body + `}`)}
				_, err = s.HTTPProgramCall(ctx, source, def, call)
				cancel()
				require.ErrorIs(t, err, streamexec.ErrTimeout, phase)
				require.Equal(t, before+1, requests.Load(), "timeout must not automatically retry, including POST")
			}
			// A transport timeout can precede the still-live context deadline.
			transport.CloseIdleConnections()
			transport.ResponseHeaderTimeout = 30 * time.Millisecond
			_, err = s.HTTPProgramCall(context.Background(), source, def, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"path":"headers","headers":{"Authorization":"Bearer private-fixture-only"}}`)})
			require.ErrorIs(t, err, streamexec.ErrTimeout)
			transport.CloseIdleConnections()
			transport.ResponseHeaderTimeout = 5 * time.Second
			before := requests.Load()
			_, err = s.HTTPProgramCall(context.Background(), source, def, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"method":"POST","path":"broken-effect","body":"once","headers":{"Authorization":"Bearer private-fixture-only"}}`)})
			require.ErrorIs(t, err, ErrHTTPRequest)
			require.Equal(t, before+1, requests.Load(), "disconnect must not replay side effects")
			raw, err := s.HTTPProgramCall(context.Background(), source, def, streamexec.ProgramCall{Kind: "source.request", Options: json.RawMessage(`{"path":"recovered","headers":{"Authorization":"Bearer private-fixture-only"}}`)})
			require.NoError(t, err)
			require.Contains(t, string(raw), `"ok":true`)
		})
	}
}

func TestHTTPLegacyReadAndOAuthTokenTLSDeadline(t *testing.T) {
	var requests atomic.Int32
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/body") {
			_, _ = w.Write([]byte(`{"partial":`))
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer fixture.Close()
	s := New(nil, nil, "")
	s.httpClient = fixture.Client()
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			def := SourceDefinition{Protocol: HTTP, Endpoint: fixture.URL, HTTP: &HTTPConnection{Authentication: "none"}}
			_, err := s.readHTTPSource(ctx, 1, def, json.RawMessage(`{"path":"`+phase+`"}`))
			cancel()
			require.ErrorIs(t, err, streamexec.ErrTimeout)
			ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
			def.Protocol = OAuth
			def.HTTP.OAuth = &OAuthConfig{ClientID: "private-fixture-client", TokenURL: fixture.URL + "/" + phase}
			_, err = s.oauthToken(ctx, def, "private-fixture-secret", url.Values{})
			cancel()
			require.ErrorIs(t, err, streamexec.ErrTimeout)
			require.NotContains(t, err.Error(), "private-fixture")
		})
	}
	require.Equal(t, int32(4), requests.Load(), "no token/request retries added")
}

func TestHTTPClientScopeParsingAndRebinding(t *testing.T) {
	for _, options := range []string{`null`, `[]`, `{"method":"TRACE"}`, `{"method":"get"}`, `{"url":"https://evil.test"}`, `{"json":{},"body":"x"}`, `{"method":"GET","body":"x"}`, `{"headers":{"Host":"evil.test"}}`, `{"headers":{"X-Key":"bad\r\nvalue"}}`, `{"headers":{"X bad":"x"}}`, `{"headers":{"Authorization":"one","authorization":"two"}}`} {
		_, err := parseHTTPRequest(json.RawMessage(options))
		require.Error(t, err, options)
	}
	for _, path := range []string{"https://evil.test/x", "//evil.test/x", "../x", "%2e%2e/x", "%252e%252e/x", "x?secret=v", "x#y", "x\\y"} {
		_, err := httpRequestURL("http://10.9.0.1/api", HTTPRequestOptions{Path: path})
		require.Error(t, err, path)
	}
	actual, err := httpRequestURL("http://10.9.0.1/api", HTTPRequestOptions{Path: "/items"})
	require.NoError(t, err)
	require.Equal(t, "http://10.9.0.1/api/items", actual)
	for _, ip := range []string{"127.0.0.1", "::1", "169.254.169.254", "100.100.100.200", "fd00:ec2::254", "0.0.0.0", "224.0.0.1"} {
		require.False(t, httpSourceAddressAllowed("fixture.test", netip.MustParseAddr(ip)), ip)
	}
	require.True(t, httpSourceAddressAllowed("fixture.internal", netip.MustParseAddr("10.9.0.1")))
	require.True(t, httpSourceAddressAllowed("fixture.test", netip.MustParseAddr("1.1.1.1")))
	require.False(t, httpSourceAddressAllowed("metadata.google.internal", netip.MustParseAddr("10.9.0.1")))
	require.False(t, httpSourceAddressAllowed("localhost", netip.MustParseAddr("10.9.0.1")))
	manager := NewHTTPClientManager()
	defer manager.Close()
	manager.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("169.254.169.254")}, nil
	}
	dialed := false
	manager.dial = func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, ErrHTTPRequest }
	client, err := manager.client(repo.Source{ID: 1, ConfigRevision: 1}, SourceDefinition{Endpoint: "http://fixture.test", HTTP: &HTTPConnection{}})
	require.NoError(t, err)
	_, err = client.Get("http://fixture.test/x")
	require.Error(t, err)
	require.False(t, dialed)
	_, err = client.Get("http://other.test/x")
	require.Error(t, err)
	require.False(t, dialed)
}

func TestHTTPClientPoolConfigurationAndSecretGuard(t *testing.T) {
	manager := NewHTTPClientManager()
	defer manager.Close()
	def := SourceDefinition{Endpoint: "https://fixture.test/api", HTTP: &HTTPConnection{Authentication: "none"}}
	source := repo.Source{ID: 1, ConfigRevision: 1}
	first, err := manager.client(source, def)
	require.NoError(t, err)
	second, err := manager.client(source, def)
	require.NoError(t, err)
	require.Same(t, first, second)
	source.ConfigRevision++
	third, err := manager.client(source, def)
	require.NoError(t, err)
	require.NotSame(t, first, third)
	def.CredentialRef = "next-reference"
	fourth, err := manager.client(source, def)
	require.NoError(t, err)
	require.NotSame(t, third, fourth)
	for id := int64(2); id < 140; id++ {
		_, err = manager.client(repo.Source{ID: id, ConfigRevision: 1}, def)
		require.NoError(t, err)
	}
	require.Len(t, manager.entries, 128)
	manager.CloseSource(139)
	require.Len(t, manager.entries, 127)
	ctx, guard := WithProgramSecretGuard(context.Background())
	continuedCtx, continuedGuard := WithProgramSecretGuard(ctx)
	require.Same(t, guard, continuedGuard, "server-preloaded secrets survive nested execution setup")
	require.Equal(t, ctx, continuedCtx)
	recordProgramSecretValues(ctx, map[string]string{"key": "named-secret", "other": "line\nsecret"})
	result := streamexec.ProgramResult{Data: json.RawMessage(`{"key":"named-secret","nested":["line\nsecret"]}`), Outputs: []streamexec.ProgramOutput{{TopicID: "7", Data: json.RawMessage(`{"token":"named-secret"}`)}}, Logs: []streamexec.Log{{Message: "echo named-secret"}}}
	guard.RedactResult(&result)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "named-secret")
	require.Contains(t, string(encoded), "[REDACTED]")
	require.JSONEq(t, `{"key":"[REDACTED]","nested":["[REDACTED]"]}`, string(result.Data))
	for _, auth := range []string{"none", "basic", "bearer", "custom", "http"} {
		def := SourceDefinition{Name: "HTTP " + auth, Protocol: HTTP, Endpoint: "http://10.9.0.1", RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: auth}}
		require.NoError(t, def.Validate())
	}
	def = SourceDefinition{Name: "Listener", Protocol: HTTP, Role: ServerRole, Endpoint: "http://managed", RuntimeLocation: "local", HTTP: &HTTPConnection{}}
	require.NoError(t, def.Validate())
	require.Equal(t, "bearer", def.HTTPAuthentication())
	def.HTTP.Anonymous = true
	require.Equal(t, "none", def.HTTPAuthentication())
	require.NoError(t, def.Validate())
	def.Endpoint = "http://10.9.0.1"
	require.Error(t, def.Validate())
}

func TestHTTPClientPinsOriginAcrossPublicToPrivateDNSChange(t *testing.T) {
	var requests, phase atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Connection", "close")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fixture.Close()
	manager := NewHTTPClientManager()
	defer manager.Close()
	manager.resolve = func(context.Context, string) ([]netip.Addr, error) {
		ip := "1.1.1.1"
		if phase.Load() == 1 {
			ip = "10.9.0.1"
		} else if phase.Load() == 2 {
			ip = "8.8.8.8"
		}
		return []netip.Addr{netip.MustParseAddr(ip)}, nil
	}
	manager.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fixture.Listener.Addr().String())
	}
	source := repo.Source{ID: 1, ConfigRevision: 1}
	def := SourceDefinition{Endpoint: "http://fixture.test/api", HTTP: &HTTPConnection{}}
	client, err := manager.client(source, def)
	require.NoError(t, err)
	read := func(client *http.Client) error {
		response, err := client.Get("http://fixture.test/api/items")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, response.Body)
		return err
	}
	require.NoError(t, read(client))
	for _, changed := range []int32{1, 2} {
		phase.Store(changed)
		require.Error(t, read(client), "an already trusted origin must not gain new IPs")
		require.Equal(t, int32(1), requests.Load())
	}
	phase.Store(0)
	require.NoError(t, read(client))
	phase.Store(1)
	source.ConfigRevision++
	refreshed, err := manager.client(source, def)
	require.NoError(t, err)
	require.NotSame(t, client, refreshed)
	require.NoError(t, read(refreshed), "explicit config refresh vets its new fixed origin set")
	require.Equal(t, int32(3), requests.Load())
}

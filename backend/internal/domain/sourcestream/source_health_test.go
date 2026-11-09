package sourcestream

// 使用本机 DNS/TLS 协议夹具和独立 PG，验证真实驱动包装、健康持久化及运行诊断；不访问外部设备。
import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/sourcehealth"
	"backend/internal/infra/edgesql"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func sqlTransportFailureEndpoints(t *testing.T) map[string]string {
	t.Helper()
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	dnsDone := make(chan struct{})
	go func() {
		defer close(dnsDone)
		for {
			buf := make([]byte, 4096)
			n, address, err := dns.ReadFrom(buf)
			if err != nil {
				return
			}
			var request dnsmessage.Message
			if request.Unpack(buf[:n]) != nil {
				continue
			}
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: request.ID, Response: true, RecursionAvailable: true, RCode: dnsmessage.RCodeNameError}, Questions: request.Questions}
			data, err := response.Pack()
			if err == nil {
				_, _ = dns.WriteTo(data, address)
			}
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dns.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = previous
		_ = dns.Close()
		<-dnsDone
	})
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	serverTLS := certificate.TLS.Clone()
	certificate.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var connections sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer connections.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				var request [8]byte
				if _, err := io.ReadFull(conn, request[:]); err != nil || binary.BigEndian.Uint32(request[:4]) != 8 || binary.BigEndian.Uint32(request[4:]) != 80877103 {
					return
				}
				if _, err := conn.Write([]byte{'S'}); err == nil {
					_ = tls.Server(conn, serverTLS).Handshake()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		connections.Wait()
	})
	return map[string]string{"dns": "postgresql://private-source.invalid:5432/private_database", "tls": "postgresql://" + listener.Addr().String() + "/private_database"}
}

func sqlTransportDefinition(endpoint string, named bool) SourceDefinition {
	options := &SQLConnection{Query: edgesql.Tools}
	if named {
		options = &SQLConnection{Queries: map[string]string{"sample": "SELECT 1"}}
	}
	return SourceDefinition{Name: "transport failure", Protocol: EdgeSQL, RuntimeLocation: "local", Endpoint: endpoint, SQL: options}
}

func assertSQLTransportDiagnostic(t *testing.T, kind string, err error) *RunError {
	t.Helper()
	diagnostic := SourceReadDiagnostic(err)
	require.NotNil(t, diagnostic)
	want := map[string]string{"dns": "SOURCE_DNS_FAILED", "tls": "SOURCE_TLS_FAILED"}[kind]
	require.Equal(t, want, diagnostic.Code)
	require.Equal(t, "source_"+kind, diagnostic.Stage)
	for current := err; current != nil; current = errors.Unwrap(current) {
		require.NotContains(t, current.Error(), "private")
		require.NotContains(t, current.Error(), "127.0.0.1")
	}
	return diagnostic
}

func TestSQLTransportFailureProbes(t *testing.T) {
	endpoints := sqlTransportFailureEndpoints(t)
	service := &Service{}
	credential := Credential{Kind: "basic", Username: "private_user", Secret: "private_password"}
	for kind, endpoint := range endpoints {
		for _, named := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/named=%t", kind, named), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_, err := service.checkConnection(ctx, 0, sqlTransportDefinition(endpoint, named), credential)
				assertSQLTransportDiagnostic(t, kind, err)
			})
		}
	}
}

func TestSQLTransportFailureHealthAndRunsPostgres(t *testing.T) {
	testkit.NewTestDB(t)
	endpoints := sqlTransportFailureEndpoints(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.source": {}}})
	service := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	defer service.Close()
	for kind, endpoint := range endpoints {
		for _, named := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/named=%t", kind, named), func(t *testing.T) {
				definition := sqlTransportDefinition(endpoint, named)
				definition.Name = fmt.Sprintf("%s named %t", kind, named)
				created, err := service.CreateSource(ctx, definition, 0)
				require.NoError(t, err)
				id := created["id"].(int64)
				saved, err := service.SaveCredential(ctx, id, created["revision"].(int64), Credential{Kind: "basic", Username: "private_user", Secret: "private_password"}, "")
				require.NoError(t, err)
				for _, mode := range []string{sourcehealth.Connection, sourcehealth.Data} {
					result, err := service.CheckSource(ctx, id, mode, 3000)
					require.NoError(t, err)
					require.Equal(t, "unhealthy", result.Health)
					require.Equal(t, kind, result.Stage)
					require.Equal(t, "SOURCE_"+map[string]string{"dns": "DNS", "tls": "TLS"}[kind]+"_FAILED", result.ErrorCode)
					stored, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
					require.NoError(t, err)
					if mode == sourcehealth.Connection {
						require.Equal(t, result, *stored.ConnectionCheck)
					} else {
						require.Equal(t, result, *stored.DataCheck)
					}
				}
				ref := edgesql.Tools
				if named {
					ref = "sql:sample"
				}
				limit := 1
				_, readErr := service.EdgeResourceRead(ctx, id, saved["configRevision"].(int64), ref, EdgeResourceReadParams{Limit: &limit}, 3000)
				diagnostic := assertSQLTransportDiagnostic(t, kind, readErr)
				run := repo.StreamRun{ID: fmt.Sprintf("%s-%t", kind, named), StreamID: id, VersionID: id + 100, EntryID: "run", Model: "program", Status: "processing", Summary: `{}`}
				reserved, err := repo.NewSourceStreamRepo(ctx).ReserveRun(ctx, run)
				require.NoError(t, err)
				require.True(t, reserved)
				observed := &programHostDiagnostics{}
				host := observed.wrap(func(context.Context, streamexec.ProgramCall) (json.RawMessage, error) { return nil, readErr })
				_, err = host(ctx, streamexec.ProgramCall{Kind: "source.read"})
				require.ErrorIs(t, err, readErr)
				err = service.failProgramV2Run(ctx, run, observed.failure(streamexec.ErrExecution))
				require.ErrorIs(t, err, ErrExecutionFailed)
				storedRun, err := repo.NewSourceStreamRepo(ctx).Run(ctx, run.ID)
				require.NoError(t, err)
				view := runView(storedRun)
				require.Equal(t, "EXECUTION_FAILED", view["error"].(*RunError).Code)
				require.Equal(t, diagnostic.Code, view["diagnostics"].(map[string]any)["hostError"].(*RunError).Code)
				require.Equal(t, "source_"+kind, view["diagnostics"].(map[string]any)["hostError"].(*RunError).Stage)
				encoded, err := json.Marshal(view)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "private")
			})
		}
	}
}

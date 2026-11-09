package httpserver

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/fleet-agent/internal/ha"
	"backend/fleet-agent/internal/status"
	"backend/internal/fleetprotocol"
)

type testHAProvider struct {
	eligible  bool
	reconcile int
}

type testHASynchronizer struct{}

func (testHASynchronizer) SyncFileSet(context.Context, string) error {
	return nil
}

func (testHASynchronizer) SyncFileSetNow(context.Context, string) error {
	return nil
}

func (testHASynchronizer) History(
	context.Context,
	string,
	string,
	int,
	int,
) (fleetprotocol.HASyncHistoryResponse, error) {
	return fleetprotocol.HASyncHistoryResponse{
		Items: []fleetprotocol.HASyncHistoryItem{{
			ID:          1,
			OperationID: "operation-1",
			Peer:        "192.0.2.10",
			FileSet:     "license",
			Status:      "succeeded",
		}},
		Total:    1,
		Page:     1,
		PageSize: 20,
	}, nil
}

func (provider *testHAProvider) Eligibility(context.Context) ha.Eligibility {
	return ha.Eligibility{Enabled: true, Eligible: provider.eligible}
}

func (provider *testHAProvider) Status() ha.Status {
	return ha.Status{Enabled: provider.eligible}
}

func (provider *testHAProvider) Containers(context.Context) (fleetprotocol.HAContainerSnapshot, error) {
	return fleetprotocol.HAContainerSnapshot{}, nil
}

func (provider *testHAProvider) Transition(context.Context, ha.TransitionRequest) error {
	return nil
}

func (provider *testHAProvider) Reconcile(context.Context) {
	provider.reconcile++
}

func TestHAVIPReadinessIsLocalAndUsesHTTPStatus(t *testing.T) {
	provider := &testHAProvider{eligible: true}
	server := New(
		"127.0.0.1:0",
		nil,
		func() status.Snapshot { return status.Snapshot{} },
		nil,
		nil,
		provider,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	request := httptest.NewRequest(http.MethodGet, "/v1/ha/vip-ready", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected eligible local request to return 200, got %d", response.Code)
	}

	provider.eligible = false
	response = httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected ineligible local request to return 503, got %d", response.Code)
	}

	request.RemoteAddr = "203.0.113.10:12345"
	response = httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected remote request to return 403, got %d", response.Code)
	}
}

func TestHAReconcileIsLocalOnly(t *testing.T) {
	provider := &testHAProvider{eligible: true}
	server := New(
		"127.0.0.1:0",
		nil,
		func() status.Snapshot { return status.Snapshot{} },
		nil,
		nil,
		provider,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	request := httptest.NewRequest(http.MethodPost, "/v1/ha/reconcile", nil)
	request.RemoteAddr = "[::1]:12345"
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || provider.reconcile != 1 {
		t.Fatalf("expected local reconcile to return 204 and run once, got status=%d count=%d", response.Code, provider.reconcile)
	}

	request.RemoteAddr = "203.0.113.10:12345"
	response = httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || provider.reconcile != 1 {
		t.Fatalf("expected remote reconcile to be rejected, got status=%d count=%d", response.Code, provider.reconcile)
	}
}

func TestHASyncHistoryRequiresLocalTokenAndReturnsPage(t *testing.T) {
	tokenHash := sha256.Sum256([]byte("local-token"))
	server := New(
		"127.0.0.1:0",
		tokenHash[:],
		func() status.Snapshot { return status.Snapshot{} },
		nil,
		nil,
		nil,
		testHASynchronizer{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	request := httptest.NewRequest(http.MethodGet, "/v1/ha/sync/history?page=1&pageSize=20", nil)
	request.Header.Set("Authorization", "Bearer local-token")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected sync history to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, `"operationId":"operation-1"`) {
		t.Fatalf("sync history response is incomplete: %s", body)
	}
}

func TestOptionalHAComponentsTreatTypedNilAsDisabled(t *testing.T) {
	tokenHash := sha256.Sum256([]byte("local-token"))
	var provider *testHAProvider
	var synchronizer *testHASynchronizer
	server := New(
		"127.0.0.1:0",
		tokenHash[:],
		func() status.Snapshot { return status.Snapshot{} },
		nil,
		nil,
		provider,
		synchronizer,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	t.Run("status", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/v1/ha/status", nil)
		request.Header.Set("Authorization", "Bearer local-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected disabled HA status to return 200, got %d: %s", response.Code, response.Body.String())
		}
		if body := response.Body.String(); !strings.Contains(body, `"enabled":false`) {
			t.Fatalf("expected disabled HA status, got %s", body)
		}
	})

	t.Run("sync history", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/v1/ha/sync/history", nil)
		request.Header.Set("Authorization", "Bearer local-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("expected disabled HA sync history to return 409, got %d: %s", response.Code, response.Body.String())
		}
	})
}

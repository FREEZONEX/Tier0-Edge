package httpserver

import (
	"backend/fleet-agent/internal/hostinfo"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/fleet-agent/internal/ha"
	"backend/fleet-agent/internal/status"
	"backend/internal/fleetprotocol"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 130 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 32 << 10
	maxRequestBody    = 64 << 10
)

type statusProvider func() status.Snapshot

type containerProvider interface {
	Snapshot(context.Context) (fleetcontainer.Snapshot, error)
	Detail(context.Context, string) (fleetcontainer.Detail, error)
	Logs(context.Context, string, fleetcontainer.LogsRequest) (fleetcontainer.LogsResult, error)
	Restart(context.Context, string, fleetcontainer.RestartRequest) (fleetcontainer.RestartResult, error)
}

type connectionConfigurator interface {
	Configure(context.Context, string) error
	Disconnect(context.Context) error
}

type haProvider interface {
	Eligibility(context.Context) ha.Eligibility
	Status() ha.Status
	Containers(context.Context) (fleetprotocol.HAContainerSnapshot, error)
	Transition(context.Context, ha.TransitionRequest) error
	Reconcile(context.Context)
}

type haSynchronizer interface {
	SyncFileSet(context.Context, string) error
	SyncFileSetNow(context.Context, string) error
	History(context.Context, string, string, int, int) (fleetprotocol.HASyncHistoryResponse, error)
}

type networkProvider interface {
	NetworkStatus(context.Context) (*fleetprotocol.NetworkStatus, error)
	EnrollNetwork(context.Context, fleetprotocol.NetworkEnrollRequest) (*fleetprotocol.NetworkStatus, error)
}

type Server struct {
	runtime        fleetprotocol.RuntimeProvider
	network        networkProvider
	httpServer     *http.Server
	localTokenHash []byte
	status         statusProvider
	containers     containerProvider
	connection     connectionConfigurator
	ha             haProvider
	syncer         haSynchronizer
	logger         *slog.Logger
}

func New(
	address string,
	localTokenHash []byte,
	provider statusProvider,
	containers containerProvider,
	connection connectionConfigurator,
	haController haProvider,
	syncer haSynchronizer,
	logger *slog.Logger,
) *Server {
	// Optional HA components are constructed as pointers in main. Converting a
	// nil pointer to one of these interfaces produces a non-nil interface, so
	// normalize typed nils once at the HTTP boundary before handlers inspect
	// them.
	if isTypedNil(haController) {
		haController = nil
	}
	if isTypedNil(syncer) {
		syncer = nil
	}
	server := &Server{
		localTokenHash: append([]byte(nil), localTokenHash...),
		status:         provider,
		containers:     containers,
		connection:     connection,
		ha:             haController,
		syncer:         syncer,
		logger:         logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", server.health)
	mux.Handle("/v1/ha/vip-ready", server.requireLocalHost(http.HandlerFunc(server.getHAVIPReadiness)))
	mux.Handle("/v1/ha/reconcile", server.requireLocalHost(http.HandlerFunc(server.reconcileHA)))
	mux.Handle("/v1/network", server.requireLocalToken(http.HandlerFunc(server.networkOperation)))
	mux.Handle("/v1/status", server.requireLocalToken(http.HandlerFunc(server.getStatus)))
	mux.Handle("/v1/runtime", server.requireLocalToken(http.HandlerFunc(server.runtimeOperation)))
	mux.Handle("/v1/host", server.requireLocalToken(http.HandlerFunc(server.getHost)))
	mux.Handle("/v1/connection", server.requireLocalToken(http.HandlerFunc(server.configureConnection)))
	mux.Handle("/v1/containers", server.requireLocalToken(http.HandlerFunc(server.getContainers)))
	mux.Handle("/v1/containers/{containerID}", server.requireLocalToken(http.HandlerFunc(server.getContainer)))
	mux.Handle("/v1/containers/{containerID}/logs", server.requireLocalToken(http.HandlerFunc(server.getContainerLogs)))
	mux.Handle("/v1/containers/{containerID}/restart", server.requireLocalToken(http.HandlerFunc(server.restartContainer)))
	mux.Handle("/v1/ha/eligibility", server.requireLocalToken(http.HandlerFunc(server.getHAEligibility)))
	mux.Handle("/v1/ha/status", server.requireLocalToken(http.HandlerFunc(server.getHAStatus)))
	mux.Handle("/v1/ha/containers", server.requireLocalToken(http.HandlerFunc(server.getHAContainers)))
	mux.Handle("/v1/ha/transition", server.requireLocalToken(http.HandlerFunc(server.transitionHA)))
	mux.Handle("/v1/ha/sync", server.requireLocalToken(http.HandlerFunc(server.syncHAFileSet)))
	mux.Handle("/v1/ha/sync/history", server.requireLocalToken(http.HandlerFunc(server.getHASyncHistory)))

	server.httpServer = &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	return server
}

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (s *Server) getHAVIPReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.ha == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if !s.ha.Eligibility(r.Context()).Eligible {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) reconcileHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.ha == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.ha.Reconcile(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getHAEligibility(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.ha == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, ha.Eligibility{
			Enabled:    false,
			Eligible:   false,
			Role:       ha.RoleStandby,
			Decision:   ha.DecisionBlockAcquire,
			ReasonCode: "FLEET_HA_DISABLED",
			CheckedAt:  time.Now().UTC().UnixMilli(),
		})
		return
	}
	result := s.ha.Eligibility(r.Context())
	statusCode := http.StatusOK
	if !result.Eligible {
		statusCode = http.StatusServiceUnavailable
	}
	s.writeJSON(w, statusCode, result)
}

func (s *Server) getHAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.ha == nil {
		s.writeJSON(w, http.StatusOK, ha.Status{
			Enabled:  false,
			Role:     ha.RoleStandby,
			Decision: ha.DecisionBlockAcquire,
		})
		return
	}
	s.writeJSON(w, http.StatusOK, s.ha.Status())
}

func (s *Server) getHAContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.ha == nil {
		s.writeError(w, http.StatusConflict, "FLEET_HA_DISABLED", "Fleet HA is not enabled")
		return
	}
	result, err := s.ha.Containers(r.Context())
	if err != nil {
		s.logger.Warn("Fleet HA container inspection failed", "error", err)
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_HA_CONTAINERS_UNAVAILABLE", "Fleet HA container status is unavailable")
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) transitionHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.ha == nil {
		s.writeError(w, http.StatusConflict, "FLEET_HA_DISABLED", "Fleet HA is not enabled")
		return
	}
	var request ha.TransitionRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "FLEET_HA_INVALID_TRANSITION", "invalid Fleet HA transition")
		return
	}
	if err := s.ha.Transition(r.Context(), request); err != nil {
		s.logger.Warn("Fleet HA transition failed", "state", request.KeepalivedState, "error", err)
		s.writeError(w, http.StatusConflict, "FLEET_HA_TRANSITION_FAILED", "Fleet HA transition failed")
		return
	}
	s.writeJSON(w, http.StatusOK, s.ha.Status())
}

func (s *Server) syncHAFileSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.ha == nil || s.syncer == nil {
		s.writeError(w, http.StatusConflict, "FLEET_HA_DISABLED", "Fleet HA rsync synchronization is not enabled")
		return
	}
	var request fleetprotocol.HASyncRequest
	if err := decodeJSONBody(w, r, &request); err != nil || strings.TrimSpace(request.FileSet) == "" {
		s.writeError(w, http.StatusBadRequest, "FLEET_HA_INVALID_SYNC", "invalid Fleet HA file set")
		return
	}
	current := s.ha.Status()
	if current.Role != ha.RoleActive {
		s.writeError(w, http.StatusConflict, "FLEET_HA_NOT_ACTIVE", "only the active Fleet HA member can start synchronization")
		return
	}
	request.FileSet = strings.TrimSpace(request.FileSet)
	var err error
	status := "queued"
	statusCode := http.StatusAccepted
	if request.Wait {
		err = s.syncer.SyncFileSetNow(r.Context(), request.FileSet)
		status = "succeeded"
		statusCode = http.StatusOK
	} else {
		err = s.syncer.SyncFileSet(r.Context(), request.FileSet)
	}
	if err != nil {
		s.logger.Warn("Fleet HA file set synchronization queue failed", "fileSet", request.FileSet, "error", err)
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_HA_SYNC_FAILED", "Fleet HA file set synchronization failed")
		return
	}
	s.writeJSON(w, statusCode, fleetprotocol.HASyncResponse{
		FileSet: request.FileSet,
		Status:  status,
	})
}

func (s *Server) getHASyncHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.syncer == nil {
		s.writeError(w, http.StatusConflict, "FLEET_HA_DISABLED", "Fleet HA rsync synchronization is not enabled")
		return
	}
	page, err := positiveQueryInteger(r, "page", 1)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "FLEET_HA_INVALID_HISTORY_QUERY", "invalid Fleet HA sync history page")
		return
	}
	pageSize, err := positiveQueryInteger(r, "pageSize", 20)
	if err != nil || pageSize > 100 {
		s.writeError(w, http.StatusBadRequest, "FLEET_HA_INVALID_HISTORY_QUERY", "invalid Fleet HA sync history page size")
		return
	}
	result, err := s.syncer.History(
		r.Context(),
		r.URL.Query().Get("peer"),
		r.URL.Query().Get("fileSet"),
		page,
		pageSize,
	)
	if err != nil {
		s.logger.Warn("Fleet HA sync history lookup failed", "error", err)
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_HA_SYNC_HISTORY_UNAVAILABLE", "Fleet HA sync history is unavailable")
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) configureConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeMethodNotAllowed(w, http.MethodPut, http.MethodDelete)
		return
	}
	if s.connection == nil {
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_AGENT_UNAVAILABLE", "Fleet connection configuration is unavailable")
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.connection.Disconnect(r.Context()); err != nil {
			s.logger.Warn("Fleet connection disconnect failed", "error", err)
			s.writeError(w, http.StatusServiceUnavailable, "FLEET_AGENT_UNAVAILABLE", "Fleet connection disconnect failed")
			return
		}
		s.writeJSON(w, http.StatusOK, s.status())
		return
	}
	var request struct {
		AccessToken string `json:"accessToken"`
	}
	if err := decodeJSONBody(w, r, &request); err != nil || strings.TrimSpace(request.AccessToken) == "" {
		s.writeError(w, http.StatusBadRequest, "FLEET_INVALID_TOKEN", "invalid Fleet Access Token")
		return
	}
	if err := s.connection.Configure(r.Context(), request.AccessToken); err != nil {
		s.logger.Warn("Fleet connection configuration failed", "error", err)
		code := fleetprotocol.ConnectionDiagnostic(err)
		if code == "" {
			code = "FLEET_CONNECTION_PREPARE_FAILED"
		}
		status := http.StatusServiceUnavailable
		if code == "FLEET_INVALID_TOKEN" {
			status = http.StatusBadRequest
		}
		s.writeError(w, status, code, code)
		return
	}
	s.writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) ListenAndServe() error {
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	current := s.status()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"service": current.Service,
		"status":  "ok",
		"version": current.Version,
		"time":    time.Now().UnixMilli(),
	})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	current := s.status()
	current.Host = hostinfo.Collect()
	s.writeJSON(w, http.StatusOK, current)
}

func (s *Server) getHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	s.writeJSON(w, http.StatusOK, hostinfo.Collect())
}

func (s *Server) getContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.containers == nil {
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_DOCKER_UNAVAILABLE", "container discovery is unavailable")
		return
	}
	snapshot, err := s.containers.Snapshot(r.Context())
	if err != nil {
		s.writeContainerError(w, "list", err)
		return
	}
	s.writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) getContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.containers == nil {
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_DOCKER_UNAVAILABLE", "container discovery is unavailable")
		return
	}
	detail, err := s.containers.Detail(r.Context(), r.PathValue("containerID"))
	if err != nil {
		s.writeContainerError(w, "detail", err)
		return
	}
	s.writeJSON(w, http.StatusOK, detail)
}

func (s *Server) getContainerLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.containers == nil {
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_DOCKER_UNAVAILABLE", "container discovery is unavailable")
		return
	}
	var request fleetcontainer.LogsRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "FLEET_INVALID_REQUEST", "invalid container logs request")
		return
	}
	result, err := s.containers.Logs(r.Context(), r.PathValue("containerID"), request)
	if err != nil {
		s.writeContainerError(w, "logs", err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) restartContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.containers == nil {
		s.writeError(w, http.StatusServiceUnavailable, "FLEET_DOCKER_UNAVAILABLE", "container operations are unavailable")
		return
	}
	var request fleetcontainer.RestartRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "FLEET_INVALID_REQUEST", "invalid container restart request")
		return
	}
	result, err := s.containers.Restart(r.Context(), r.PathValue("containerID"), request)
	if err != nil {
		s.writeContainerError(w, "restart", err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeContainerError(w http.ResponseWriter, operation string, err error) {
	statusCode := http.StatusServiceUnavailable
	code := "FLEET_DOCKER_UNAVAILABLE"
	message := "Docker Engine is unavailable"
	switch {
	case errors.Is(err, fleetcontainer.ErrUnsupported):
		statusCode = http.StatusNotImplemented
		code = "FLEET_CONTAINER_UNSUPPORTED"
		message = "container operations are not supported by this installation"
	case errors.Is(err, fleetcontainer.ErrInvalid):
		statusCode = http.StatusBadRequest
		code = "FLEET_INVALID_REQUEST"
		message = "invalid container request"
	case errors.Is(err, fleetcontainer.ErrNotFound):
		statusCode = http.StatusNotFound
		code = "FLEET_CONTAINER_NOT_FOUND"
		message = "container was not found"
	case errors.Is(err, fleetcontainer.ErrNotManaged):
		statusCode = http.StatusForbidden
		code = "FLEET_CONTAINER_NOT_MANAGED"
		message = "container is not managed by this installation"
	case errors.Is(err, fleetcontainer.ErrRestartDenied):
		statusCode = http.StatusForbidden
		code = "FLEET_CONTAINER_RESTART_FORBIDDEN"
		message = "container restart is not allowed"
	case errors.Is(err, fleetcontainer.ErrConflict):
		statusCode = http.StatusConflict
		code = "FLEET_CONTAINER_OPERATION_CONFLICT"
		message = "another container operation is already running"
	case errors.Is(err, fleetcontainer.ErrInterrupted):
		statusCode = http.StatusConflict
		code = "FLEET_OPERATION_INTERRUPTED"
		message = "container operation was interrupted before completion"
	case errors.Is(err, fleetcontainer.ErrNotConfigured):
		code = "FLEET_INSTALLATION_ID_NOT_CONFIGURED"
		message = "container discovery requires an installation ID"
	case errors.Is(err, context.DeadlineExceeded):
		statusCode = http.StatusGatewayTimeout
		code = "FLEET_OPERATION_TIMEOUT"
		message = "container operation timed out"
	case errors.Is(err, context.Canceled):
		statusCode = http.StatusRequestTimeout
		code = "FLEET_OPERATION_CANCELED"
		message = "container operation was canceled"
	}
	s.logger.Warn("container API request failed", "operation", operation, "code", code)
	s.writeError(w, statusCode, code, message)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func positiveQueryInteger(r *http.Request, name string, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("query value must be a positive integer")
	}
	return parsed, nil
}

func (s *Server) requireLocalToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.localTokenHash) != sha256.Size {
			s.writeError(w, http.StatusServiceUnavailable, "FLEET_LOCAL_AUTH_NOT_CONFIGURED", "local API authentication is not configured")
			return
		}

		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, prefix) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.writeError(w, http.StatusUnauthorized, "FLEET_LOCAL_AUTH_INVALID", "missing or invalid local API credentials")
			return
		}

		provided := strings.TrimPrefix(authorization, prefix)
		providedHash := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(providedHash[:], s.localTokenHash) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.writeError(w, http.StatusUnauthorized, "FLEET_LOCAL_AUTH_INVALID", "missing or invalid local API credentials")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireLocalHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !isLocalHostAddress(host) {
			s.writeError(w, http.StatusForbidden, "FLEET_LOCAL_HOST_REQUIRED", "endpoint is only available from this host")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalHostAddress(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		var localIP net.IP
		switch value := address.(type) {
		case *net.IPNet:
			localIP = value.IP
		case *net.IPAddr:
			localIP = value.IP
		}
		if localIP != nil && localIP.Equal(ip) {
			return true
		}
	}
	return false
}

func writeMethodNotAllowed(w http.ResponseWriter, allowedMethods ...string) {
	w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "FLEET_METHOD_NOT_ALLOWED", "method not allowed")
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(w, statusCode, map[string]string{
		"code":    code,
		"message": message,
	})
}

func (s *Server) writeError(w http.ResponseWriter, statusCode int, code, message string) {
	s.writeJSON(w, statusCode, map[string]string{
		"code":    code,
		"message": message,
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Error("write HTTP response", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("write HTTP response", "error", err)
	}
}

func (s *Server) BindNetwork(network networkProvider) { s.network = network }
func (s *Server) networkOperation(w http.ResponseWriter, r *http.Request) {
	if s.network == nil {
		http.Error(w, "FLEET_NETWORK_UNAVAILABLE", 503)
		return
	}
	var result *fleetprotocol.NetworkStatus
	var err error
	switch r.Method {
	case http.MethodGet:
		result, err = s.network.NetworkStatus(r.Context())
		// An unavailable network is a valid observation. Preserve its diagnostic
		// fields instead of discarding them as a failed status request.
		if result != nil {
			s.writeJSON(w, http.StatusOK, result)
			return
		}
	case http.MethodPut:
		var request fleetprotocol.NetworkEnrollRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "FLEET_NETWORK_REQUEST_INVALID", 400)
			return
		}
		result, err = s.network.EnrollNetwork(r.Context(), request)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
		return
	}
	if err != nil {
		s.writeJSON(w, 503, map[string]any{"code": "FLEET_NETWORK_UNAVAILABLE", "message": err.Error(), "network": result})
		return
	}
	s.writeJSON(w, 200, result)
}

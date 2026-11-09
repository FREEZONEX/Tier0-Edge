//go:build windows || darwin || linux

package main

import (
	"backend/internal/fleetprotocol"
	"backend/internal/product"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type managedService struct {
	exe      string
	args     []string
	cmd      *exec.Cmd
	job      processJob
	done     chan struct{}
	stopping bool
	item     fleetprotocol.RuntimeItem
}

// Only this launcher's registered children are addressable. The random endpoint
// credential is inherited exclusively by the matching Fleet Agent.
func (s *supervisor) serveRuntime() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.runtimeURL = "http://" + listener.Addr().String()
	s.runtimeToken = token()
	s.operations = map[string]fleetprotocol.RuntimeOperation{}
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			http.Error(w, `{"code":"RUNTIME_INVALID_REQUEST"}`, 400)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.runtimeToken)) != 1 {
			http.Error(w, `{"code":"RUNTIME_UNAUTHORIZED"}`, 401)
			return
		}
		var request fleetprotocol.RuntimeRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, `{"code":"RUNTIME_INVALID_REQUEST"}`, 400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, `{"code":"RUNTIME_INVALID_REQUEST"}`, 400)
			return
		}
		result, err := fleetprotocol.ExecuteRuntime(r.Context(), s, request)
		if err != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})}
	go server.Serve(listener)
	go func() { <-s.ctx.Done(); _ = server.Close() }()
	return nil
}

func (s *supervisor) actionsLocked(id string) ([]string, string) {
	service := s.services[id]
	actions := []string{"logs"}
	if !s.ready || s.closing {
		return actions, "RUNTIME_NOT_READY"
	}
	if id == "fleet" {
		return actions, "RUNTIME_MANAGEMENT_CONNECTION_REQUIRED"
	}
	dependencyID := func(id string) string {
		if id == "timescale" {
			return "postgres"
		}
		return id
	}
	if service.cmd == nil {
		for _, dependency := range product.Service(id).Dependencies {
			// Node/web/extensions are packaged components, not supervisor processes.
			if child, ok := s.services[dependencyID(dependency)]; ok && child.cmd == nil {
				return actions, "RUNTIME_DEPENDENCY_UNAVAILABLE"
			}
		}
		return append(actions, "start"), ""
	}
	if id == "backend" {
		return append(actions, "restart"), "RUNTIME_STOP_WOULD_DISCONNECT"
	}
	for other, child := range s.services {
		if other == id || child.cmd == nil {
			continue
		}
		for _, dependency := range product.Service(other).Dependencies {
			if dependencyID(dependency) == id {
				return actions, "RUNTIME_DEPENDENTS_RUNNING"
			}
		}
	}
	return append(actions, "stop", "restart"), ""
}

func (s *supervisor) List(ctx context.Context) (fleetprotocol.RuntimeSnapshot, error) {
	result := fleetprotocol.RuntimeSnapshot{RuntimeMode: "native", ObservedAt: time.Now().UnixMilli(), Items: []fleetprotocol.RuntimeItem{}}
	s.mu.Lock()
	for id, service := range s.services {
		item := service.item
		component := product.Service(id)
		item.InstallationID = s.installationID
		item.Dependencies = component.Dependencies
		item.Protection = component.Protection
		item.Actions, item.Reason = s.actionsLocked(id)
		item.Health = "unknown"
		if item.State != "running" {
			item.Health = "unavailable"
		}
		result.Items = append(result.Items, item)
	}
	s.mu.Unlock()
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].ID < result.Items[j].ID })
	for i := range result.Items {
		item := &result.Items[i]
		if item.State != "running" {
			continue
		}
		port := map[string]string{"postgres": "15432", "nats": "14222", "mqtt": "11883", "fleet": "19732"}[item.ID]
		if port != "" {
			conn, err := (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(ctx, "tcp", "127.0.0.1:"+servicePort(port))
			item.Health = "unavailable"
			if err == nil {
				conn.Close()
				item.Health = "ready"
			}
		}
	}
	return result, nil
}

func (s *supervisor) Logs(_ context.Context, request fleetprotocol.RuntimeRequest) (fleetprotocol.RuntimeLogs, error) {
	s.mu.Lock()
	service := s.services[request.ResourceID]
	s.mu.Unlock()
	result := fleetprotocol.RuntimeLogs{ResourceID: request.ResourceID}
	if service == nil {
		return result, errors.New("RUNTIME_RESOURCE_NOT_FOUND")
	}
	f, err := os.Open(filepath.Join(s.root, "logs", request.ResourceID+".log"))
	if err != nil {
		return result, errors.New("RUNTIME_LOG_UNAVAILABLE")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return result, errors.New("RUNTIME_LOG_UNAVAILABLE")
	}
	const limit int64 = 256 * 1024
	offset := int64(0)
	if stat.Size() > limit {
		offset = stat.Size() - limit
		result.Truncated = true
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return result, errors.New("RUNTIME_LOG_UNAVAILABLE")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return result, errors.New("RUNTIME_LOG_UNAVAILABLE")
	}
	lines := strings.Split(strings.ToValidUTF8(string(raw), "�"), "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:]
	}
	tail := request.Tail
	if tail == 0 {
		tail = 200
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
		result.Truncated = true
	}
	result.Text = strings.Join(lines, "\n")
	for _, entry := range s.env {
		key, value, _ := strings.Cut(entry, "=")
		if len(value) >= 6 && (strings.Contains(key, "PASSWORD") || strings.Contains(key, "SECRET") || strings.Contains(key, "TOKEN") || strings.HasSuffix(key, "_KEY")) {
			result.Text = strings.ReplaceAll(result.Text, value, "[redacted]")
		}
	}
	result.Text = runtimeLogCredential.ReplaceAllString(result.Text, "${1}[redacted]")
	return result, nil
}

var runtimeLogCredential = regexp.MustCompile(`(?i)((?:password|secret|token|authorization|api[_-]?key)["']?\s*[:=]\s*["']?)([^\s,"'}]+)`)

func (s *supervisor) Control(_ context.Context, request fleetprotocol.RuntimeRequest) (fleetprotocol.RuntimeOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.operations[request.OperationID]; ok {
		if prior.ResourceID != request.ResourceID || prior.Verb != request.Verb {
			return prior, errors.New("RUNTIME_OPERATION_CONFLICT")
		}
		return prior, nil
	}
	if s.services[request.ResourceID] == nil {
		return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_RESOURCE_NOT_FOUND")
	}
	actions, reason := s.actionsLocked(request.ResourceID)
	allowed := false
	for _, action := range actions {
		if action == request.Verb {
			allowed = true
		}
	}
	if !allowed {
		if reason == "" {
			reason = "RUNTIME_ACTION_UNAVAILABLE"
		}
		return fleetprotocol.RuntimeOperation{}, errors.New(reason)
	}
	for _, op := range s.operations {
		if op.State == "pending" {
			return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_OPERATION_IN_PROGRESS")
		}
	}
	for id, op := range s.operations {
		if op.State != "pending" && time.Since(time.UnixMilli(op.UpdatedAt)) > 24*time.Hour {
			delete(s.operations, id)
		}
	}
	if len(s.operations) >= 2048 {
		return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_OPERATION_LIMIT")
	}
	op := fleetprotocol.RuntimeOperation{ID: request.OperationID, ResourceID: request.ResourceID, Verb: request.Verb, State: "pending", UpdatedAt: time.Now().UnixMilli()}
	s.operations[op.ID] = op
	go func() {
		s.controlMu.Lock()
		defer s.controlMu.Unlock()
		var err error
		if request.Verb != "start" {
			err = s.stopService(request.ResourceID)
		}
		if err == nil && request.Verb != "stop" {
			err = s.startService(request.ResourceID)
		}
		if err == nil && request.Verb != "stop" {
			err = s.awaitService(request.ResourceID)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		op.State = "succeeded"
		if err != nil {
			op.State = "failed"
			op.Error = err.Error()
		}
		op.UpdatedAt = time.Now().UnixMilli()
		s.operations[op.ID] = op
	}()
	return op, nil
}

func (s *supervisor) Operation(_ context.Context, id string) (fleetprotocol.RuntimeOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op, ok := s.operations[id]; ok {
		return op, nil
	}
	return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_OPERATION_UNKNOWN")
}

func (s *supervisor) stopService(id string) error {
	s.mu.Lock()
	child := s.services[id]
	if child == nil || child.cmd == nil {
		s.mu.Unlock()
		return nil
	}
	child.stopping = true
	done := child.done
	processID := child.cmd.Process.Pid
	if id != "postgres" {
		err := terminateService(child)
		s.mu.Unlock()
		if err != nil {
			return errors.New("RUNTIME_STOP_FAILED")
		}
	} else {
		s.mu.Unlock()
		raw, err := os.ReadFile(filepath.Join(s.root, "data", "postgres", "postmaster.pid"))
		if err != nil {
			return errors.New("RUNTIME_POSTGRES_IDENTITY_UNAVAILABLE")
		}
		pid, _ := strconv.Atoi(strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0])
		if pid != processID {
			return errors.New("RUNTIME_POSTGRES_IDENTITY_MISMATCH")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := s.command(filepath.Join(s.programRoot, "postgres", "bin", binaryName("pg_ctl")), "-D", filepath.Join(s.root, "data", "postgres"), "stop", "-m", "fast", "-w", "-t", "15")
		c.WaitDelay = time.Second
		// Use a bounded context without exposing the private environment in logs.
		if err := c.Start(); err != nil {
			return errors.New("RUNTIME_POSTGRES_STOP_FAILED")
		}
		waited := make(chan error, 1)
		go func() { waited <- c.Wait() }()
		select {
		case err := <-waited:
			if err != nil {
				return errors.New("RUNTIME_POSTGRES_STOP_FAILED")
			}
		case <-ctx.Done():
			_ = c.Process.Kill()
			return errors.New("RUNTIME_POSTGRES_STOP_TIMEOUT")
		}
	}
	select {
	case <-done:
		return nil
	case <-time.After(20 * time.Second):
		return errors.New("RUNTIME_STOP_TIMEOUT")
	}
}

func (s *supervisor) awaitService(id string) error {
	// A PID alone is not an HTTP readiness result. Backends can take time to
	// reopen the database and restore their persisted stream/app runtimes.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if s.ctx.Err() != nil {
			return errors.New("RUNTIME_SHUTTING_DOWN")
		}
		s.mu.Lock()
		child := s.services[id]
		running := child.cmd != nil
		s.mu.Unlock()
		if !running {
			return errors.New("RUNTIME_PROCESS_EXITED")
		}
		if id == "backend" {
			base := ""
			for _, e := range s.env {
				if strings.HasPrefix(e, "NATIVE_BACKEND_URL=") {
					base = strings.TrimPrefix(e, "NATIVE_BACKEND_URL=")
				}
			}
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
			response, err := client.Get(base + "/readyz")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					return nil
				}
			}
		} else if port := map[string]string{"postgres": "15432", "nats": "14222", "mqtt": "11883"}[id]; port != "" {
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+servicePort(port), 300*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		} else {
			time.Sleep(time.Second)
			s.mu.Lock()
			running = child.cmd != nil
			s.mu.Unlock()
			if running {
				return nil
			}
			return errors.New("RUNTIME_PROCESS_EXITED")
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("RUNTIME_READINESS_TIMEOUT")
}

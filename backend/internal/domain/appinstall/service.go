package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend/internal/appruntime"
	"backend/internal/infra/privatefs"
	"backend/internal/repo"
	"github.com/google/uuid"
)

type Options struct {
	Root       string
	Tools      Tools
	Repository *repo.AppInstallationRepo
	// Validate checks that the App is a local, live UNS v2 App. HTTP permission
	// checks remain mandatory at the authenticated management boundary.
	Validate    func(context.Context, string) error
	Environment func(context.Context, string) (map[string]string, error)
}
type appState struct {
	mu         sync.Mutex
	process    *appruntime.Process
	port       int
	log        *os.File
	generation uint64
	restarts   []time.Time
}
type Service struct {
	options    Options
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	states     map[string]*appState
	closed     bool
	operations sync.WaitGroup
}

func New(ctx context.Context, options Options) (*Service, error) {
	if !filepath.IsAbs(options.Root) || options.Repository == nil || options.Validate == nil {
		return nil, errors.New("native App installation configuration unavailable")
	}
	if err := os.MkdirAll(options.Root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(options.Root)
	if err != nil || !info.IsDir() || !privatefs.IsPrivate(options.Root, info) {
		return nil, errors.New("native App directory must be private to this installation")
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Service{options: options, ctx: ctx, cancel: cancel, states: map[string]*appState{}}, nil
}
func (s *Service) enter(id string) (*appState, func(), error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil, errors.New("invalid native App identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("native App service stopped")
	}
	state := s.states[id]
	if state == nil {
		state = &appState{}
		s.states[id] = state
	}
	s.operations.Add(1)
	return state, s.operations.Done, nil
}
func (s *Service) operationContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
func (s *Service) releasePath(id, release string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("invalid App identity")
	}
	if _, err := uuid.Parse(release); err != nil {
		return "", errors.New("invalid App release identity")
	}
	return filepath.Join(s.options.Root, id, "releases", release), nil
}
func (s *Service) environment(ctx context.Context, id string) (map[string]string, error) {
	if s.options.Environment == nil {
		return map[string]string{}, nil
	}
	return s.options.Environment(ctx, id)
}
func (s *Service) Install(ctx context.Context, id, filename string) (repo.AppInstallation, error) {
	return s.install(ctx, id, filename, nil)
}

func (s *Service) install(ctx context.Context, id, filename string, prepareCredentials func(context.Context, string) error) (result repo.AppInstallation, resultErr error) {
	state, leave, err := s.enter(id)
	if err != nil {
		return result, err
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	if err = s.options.Validate(ctx, id); err != nil {
		return result, err
	}
	operation := uuid.NewString()
	previous, err := s.options.Repository.Begin(ctx, id, operation)
	if err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			failureCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = s.options.Repository.Fail(failureCtx, id, operation, resultErr.Error())
		}
	}()
	file, err := os.Open(filename)
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, contextReader{ctx: ctx, reader: file})
	file.Close()
	if err != nil {
		return result, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	release := uuid.NewString()
	candidate, err := s.releasePath(id, release)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(filepath.Dir(candidate), 0700); err != nil {
		return result, err
	}
	if err = s.options.Repository.Candidate(ctx, id, operation, release); err != nil {
		return result, err
	}
	committed := false
	candidateCreated := false
	defer func() {
		if candidateCreated && !committed {
			_ = os.RemoveAll(candidate)
		}
	}()
	bundle, err := ExtractBundle(ctx, filename, candidate)
	if err != nil {
		return result, err
	}
	candidateCreated = true
	if previous.ReleaseID != "" {
		var old Bundle
		if json.Unmarshal(previous.Manifest, &old) != nil {
			return result, errors.New("saved App manifest is invalid")
		}
		if old.SourceAppID != "" && bundle.SourceAppID != "" && old.SourceAppID != bundle.SourceAppID {
			return result, errors.New("replacement package belongs to a different source App")
		}
	}
	// Each release owns its log. It remains available after failed builds and
	// never gets mixed into another App or another installation's logs.
	logs := filepath.Join(s.options.Root, id, "logs")
	if err = os.MkdirAll(logs, 0700); err != nil {
		return result, err
	}
	log, err := os.OpenFile(filepath.Join(logs, release+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	keepLog := false
	defer func() {
		if !keepLog {
			log.Close()
		}
	}()
	if err = s.options.Repository.Stage(ctx, id, operation, "building"); err != nil {
		return result, err
	}
	// 重装先登记安装操作，再准备凭据；已卸载但尚未重装的应用仍不能签发凭据。
	if prepareCredentials != nil {
		if err = prepareCredentials(ctx, id); err != nil {
			return result, err
		}
	}
	if err = Prepare(ctx, s.options.Tools, candidate, bundle, id, log); err != nil {
		return result, fmt.Errorf("App dependency/build phase failed: %w", err)
	}
	if err = s.options.Repository.Stage(ctx, id, operation, "starting"); err != nil {
		return result, err
	}
	environment, err := s.environment(ctx, id)
	if err != nil {
		return result, err
	}
	healthCtx, healthCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer healthCancel()
	process, port, err := StartCandidate(healthCtx, s.options.Tools, candidate, bundle, id, environment, log)
	if err != nil {
		return result, err
	}
	publish := false
	defer func() {
		if !publish {
			_ = process.Stop()
		}
	}()
	manifest, err := json.Marshal(bundle)
	if err != nil {
		return result, err
	}
	if err = s.options.Repository.Commit(ctx, id, operation, previous.Revision, release, manifest, digest, bundle.Name, bundle.Version); err != nil {
		return result, err
	}
	committed = true
	oldProcess, oldLog := state.process, state.log
	state.process = process
	state.port = port
	state.log = log
	state.generation++
	state.restarts = nil
	keepLog = true
	publish = true
	if oldProcess != nil {
		_ = oldProcess.Stop()
	}
	if oldLog != nil {
		_ = oldLog.Close()
	}
	go s.observe(id, state, process)
	result = previous
	result.ReleaseID = release
	result.AttemptReleaseID = release
	result.Manifest = manifest
	result.PackageSHA256 = digest
	result.Name = bundle.Name
	result.Version = bundle.Version
	result.Revision = previous.Revision + 1
	result.Status = "running"
	result.DesiredState = "running"
	result.Stage = "ready"
	result.OperationID = ""
	result.LastError = ""
	result.UpdatedAt = time.Now().UnixMilli()
	return result, nil
}
func (s *Service) observe(id string, state *appState, process *appruntime.Process) {
	select {
	case <-s.ctx.Done():
		return
	case <-process.Done():
	}
	_, leave, err := s.enter(id)
	if err != nil {
		return
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.process != process || s.ctx.Err() != nil {
		return
	}
	state.process = nil
	state.port = 0
	if state.log != nil {
		state.log.Close()
		state.log = nil
	}
	generation := state.generation
	for s.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		row, err := s.options.Repository.Get(ctx, id)
		cancel()
		if err != nil || row.DesiredState != "running" || row.ReleaseID == "" {
			return
		}
		now := time.Now()
		kept := state.restarts[:0]
		for _, at := range state.restarts {
			if now.Sub(at) < 5*time.Minute {
				kept = append(kept, at)
			}
		}
		state.restarts = kept
		ctx, cancel = context.WithTimeout(s.ctx, 5*time.Second)
		if len(state.restarts) >= 3 {
			_ = s.options.Repository.SetState(ctx, id, "", "failed", "App exited repeatedly; automatic restart limit reached (3 in 5 minutes). Start manually after checking logs")
			cancel()
			return
		}
		delay := time.Second << uint(len(state.restarts))
		state.restarts = append(state.restarts, now)
		_ = s.options.Repository.SetState(ctx, id, "", "starting", fmt.Sprintf("App process exited; restarting in %s (%d/3)", delay, len(state.restarts)))
		cancel()
		state.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		state.mu.Lock()
		// Stop, uninstall, replacement or a manual start supersedes this retry.
		if s.ctx.Err() != nil || state.generation != generation || state.process != nil {
			return
		}
		ctx, cancel = s.operationContext(s.ctx)
		err = s.options.Validate(ctx, id)
		if err == nil {
			err = s.startLocked(ctx, state, row)
		}
		cancel()
		if err == nil {
			return
		}
	}
}
func (s *Service) Start(ctx context.Context, id string) error {
	state, leave, err := s.enter(id)
	if err != nil {
		return err
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.process != nil {
		return nil
	}
	state.generation++
	state.restarts = nil
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	if err = s.options.Validate(ctx, id); err != nil {
		return err
	}
	row, err := s.options.Repository.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.startLocked(ctx, state, row)
}
func (s *Service) startLocked(ctx context.Context, state *appState, row repo.AppInstallation) (err error) {
	var bundle Bundle
	if json.Unmarshal(row.Manifest, &bundle) != nil {
		return errors.New("invalid saved App manifest")
	}
	path, err := s.releasePath(row.AppID, row.ReleaseID)
	if err != nil {
		return err
	}
	env, err := s.environment(ctx, row.AppID)
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(s.options.Root, row.AppID, "logs", row.ReleaseID+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = s.options.Repository.SetState(ctx, row.AppID, "running", "starting", ""); err != nil {
		log.Close()
		return err
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	p, port, err := StartCandidate(healthCtx, s.options.Tools, path, bundle, row.AppID, env, log)
	if err != nil {
		log.Close()
		_ = s.options.Repository.SetState(ctx, row.AppID, "running", "failed", err.Error())
		return err
	}
	if err = s.options.Repository.SetState(ctx, row.AppID, "running", "running", ""); err != nil {
		p.Stop()
		log.Close()
		return err
	}
	state.process = p
	state.port = port
	state.log = log
	state.generation++
	go s.observe(row.AppID, state, p)
	return nil
}
func (s *Service) Stop(ctx context.Context, id string) error {
	state, leave, err := s.enter(id)
	if err != nil {
		return err
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.generation++
	if err = s.options.Repository.SetState(ctx, id, "stopped", "stopped", ""); err != nil {
		return err
	}
	if state.process != nil {
		err = state.process.Stop()
		state.process = nil
		state.port = 0
	}
	if state.log != nil {
		state.log.Close()
		state.log = nil
	}
	return err
}

// Uninstall removes only recorded executable releases. Collections, uploads,
// logs and the App's persistent directory are retained.
func (s *Service) Uninstall(ctx context.Context, id string) error {
	if err := s.Stop(ctx, id); err != nil {
		return err
	}
	state, leave, err := s.enter(id)
	if err != nil {
		return err
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.generation++
	if err = s.options.Repository.Uninstall(ctx, id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.options.Root, id, "releases"))
}

// Resolve returns only live, process-owned loopback endpoints. Stored status or
// arbitrary manifest URLs are never accepted as proxy destinations.
func (s *Service) Resolve(id string) (int, error) {
	checkCtx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	if err := s.options.Validate(checkCtx, id); err != nil {
		return 0, err
	}
	state, leave, err := s.enter(id)
	if err != nil {
		return 0, err
	}
	defer leave()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.process == nil {
		return 0, errors.New("App is not running")
	}
	select {
	case <-state.process.Done():
		return 0, errors.New("App is not running")
	default:
		return state.port, nil
	}
}
func (s *Service) Restore(ctx context.Context) error {
	if err := s.options.Repository.Recover(ctx); err != nil {
		return err
	}
	rows, err := s.options.Repository.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ReleaseID == "" {
			continue
		}
		if row.DesiredState == "running" {
			if err = s.Start(ctx, row.AppID); err != nil {
				_ = s.options.Repository.SetState(ctx, row.AppID, "", "failed", err.Error())
			}
		} else {
			_ = s.options.Repository.SetState(ctx, row.AppID, "stopped", "stopped", "")
		}
	}
	return nil
}

// Logs is bounded and resolves a release from the installation record, never a
// caller-supplied path. Credential values are removed before a management view.
func (s *Service) Logs(ctx context.Context, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("invalid App identity")
	}
	if err := s.options.Validate(ctx, id); err != nil {
		return "", err
	}
	row, err := s.options.Repository.Get(ctx, id)
	if err != nil {
		return "", err
	}
	release := row.AttemptReleaseID
	if release == "" {
		release = row.ReleaseID
	}
	if _, err = s.releasePath(id, release); err != nil {
		return "", err
	}
	file, err := os.Open(filepath.Join(s.options.Root, id, "logs", release+".log"))
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	offset := info.Size() - 65536
	if offset < 0 {
		offset = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(file, offset, 65536))
	if err != nil {
		return "", err
	}
	environment, err := s.environment(ctx, id)
	if err != nil {
		return "", err
	}
	text := string(data)
	for key, value := range environment {
		key = strings.ToUpper(key)
		if len(value) >= 8 && (strings.Contains(key, "KEY") || strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD")) {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	return text, nil
}
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	states := make([]*appState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	s.mu.Unlock()
	s.operations.Wait()
	for _, state := range states {
		state.mu.Lock()
		if state.process != nil {
			_ = state.process.Stop()
			state.process = nil
			state.port = 0
		}
		if state.log != nil {
			state.log.Close()
			state.log = nil
		}
		state.mu.Unlock()
	}
}

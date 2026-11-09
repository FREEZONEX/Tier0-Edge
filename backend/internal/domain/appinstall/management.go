package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/asset"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/google/uuid"
)

type InstallRequest struct {
	SkipInvalidRoles bool          `json:"skipInvalidRoles,omitempty"`
	RolePlanDigest   string        `json:"rolePlanDigest,omitempty"`
	RenameRoleKeys   []string      `json:"renameRoleKeys,omitempty"`
	FileID           string        `json:"fileId"`
	AppID            string        `json:"appId,omitempty"`
	IdempotencyKey   string        `json:"idempotencyKey"`
	Collections      []unsv2.Grant `json:"collections,omitempty"`
	AllowReplace     bool          `json:"allowReplace,omitempty"`
	PackageIdentity  string        `json:"packageIdentity,omitempty"`
}
type Management struct {
	runtime      *Service
	credentials  *Credentials
	uns          *unsv2.Service
	sources      *sourcestream.Service
	assets       *asset.Service
	root         string
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	metadataMu   sync.Mutex
	closed       bool
	jobs         sync.WaitGroup
	installLocks map[string]*sync.Mutex
	installSlots chan struct{}
}

func NewManagement(ctx context.Context, runtime *Service, credentials *Credentials, uns *unsv2.Service, assets *asset.Service, root string, sources ...*sourcestream.Service) (*Management, error) {
	if runtime == nil || credentials == nil || uns == nil || assets == nil {
		return nil, errors.New("native App management unavailable")
	}
	if err := repo.NewAsyncJobRepo(ctx).RecoverNativeAppJobs(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Management{runtime: runtime, credentials: credentials, uns: uns, assets: assets, root: root, ctx: ctx, cancel: cancel, installLocks: map[string]*sync.Mutex{}, installSlots: make(chan struct{}, 2)}
	if len(sources) > 0 {
		m.sources = sources[0]
	}
	return m, nil
}

// SetSourceRuntime is called during bootstrap, before management HTTP routes
// are served. NativeApps itself must exist earlier when the gateway is wired.
func (m *Management) SetSourceRuntime(sources *sourcestream.Service) {
	m.sources = sources
}

func (m *Management) snapshot(ctx context.Context, fileID string) (string, error) {
	id, err := strconv.ParseInt(fileID, 10, 64)
	if err != nil || id <= 0 {
		return "", invalidRequest("invalid App file ID")
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" {
		return "", forbiddenRequest("App upload requires a platform session")
	}
	item, reader, _, err := m.assets.Open(ctx, id)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if item.CreatedBy != subject.UserID {
		return "", forbiddenRequest("App ZIP must be uploaded by the current user")
	}
	if item.SizeBytes > maxBundleBytes {
		return "", invalidRequest("App ZIP exceeds size limit")
	}
	dir := filepath.Join(m.root, "uploads")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "app-*.zip")
	if err != nil {
		return "", err
	}
	ok = false
	defer func() {
		if !ok {
			os.Remove(file.Name())
		}
	}()
	n, err := io.Copy(file, io.LimitReader(contextReader{ctx: ctx, reader: reader}, maxBundleBytes+1))
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > maxBundleBytes {
		return "", errors.New("App ZIP exceeds size limit")
	}
	ok = true
	return file.Name(), nil
}
func (m *Management) Preview(ctx context.Context, fileID string) (Bundle, error) {
	name, err := m.snapshot(ctx, fileID)
	if err != nil {
		return Bundle{}, err
	}
	defer os.Remove(name)
	bundle, err := InspectBundle(name)
	if err != nil {
		return Bundle{}, invalidRequest(err.Error())
	}
	bundle.RolePlan, err = previewRolePlan(ctx, bundle.Roles)
	if err != nil {
		return Bundle{}, err
	}
	bundle.MetadataPlan, err = m.previewMetadata(ctx, bundle.Metadata)
	if err != nil {
		return Bundle{}, err
	}
	digest, err := packageDigest(ctx, name)
	if err != nil {
		return Bundle{}, err
	}
	existing, err := m.matchingInstall(ctx, bundle, digest)
	if err != nil {
		return Bundle{}, err
	}
	if existing != nil {
		bundle.ExistingApp = &ExistingApp{AppID: existing.AppID, Name: existing.Name, Version: existing.Version, SamePackage: existing.PackageSHA256 == digest}
	}
	return bundle, nil
}
func (m *Management) Install(ctx context.Context, request InstallRequest) (map[string]any, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" || !contextx.HasResource(subject, "uns.app.manage") {
		return nil, forbiddenRequest("native App management permission required")
	}
	if strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey || len(request.IdempotencyKey) < 1 || len(request.IdempotencyKey) > 128 {
		return nil, invalidRequest("valid App idempotency key required")
	}
	request.AllowReplace = request.AppID != ""
	if err := m.resolveInstall(ctx, &request); err != nil {
		return nil, err
	}
	if request.AppID == "" {
		request.AppID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("nativeApp/package/"+request.PackageIdentity)).String()
	}
	if _, err := uuid.Parse(request.AppID); err != nil {
		return nil, invalidRequest("invalid App identity")
	}
	// Creating the job before any metadata side effect makes replay deterministic.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("native App management stopped")
	}
	job, created, err := repo.NewAsyncJobRepo(ctx).CreateNativeAppJob(ctx, request, subject.UserID, request.IdempotencyKey)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if created {
		m.jobs.Add(1)
	}
	m.mu.Unlock()
	if created {
		go func() {
			defer m.jobs.Done()
			jobCtx := contextx.WithSubject(m.ctx, subject)
			jobs := repo.NewAsyncJobRepo(jobCtx)
			select {
			case m.installSlots <- struct{}{}:
				defer func() { <-m.installSlots }()
			case <-jobCtx.Done():
				return
			}
			if err := jobs.MarkAsyncJobRunning(jobCtx, job.JobKey); err != nil {
				return
			}
			result, err := m.install(jobCtx, request)
			if err != nil {
				failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = jobs.MarkAsyncJobFailed(failureCtx, job.JobKey, err.Error(), map[string]any{"appId": request.AppID})
				return
			}
			_ = jobs.MarkAsyncJobSucceeded(jobCtx, job.JobKey, result)
		}()
	}
	return map[string]any{"jobId": job.JobKey, "appId": request.AppID, "status": job.Status, "replayed": !created}, nil
}
func (m *Management) install(ctx context.Context, request InstallRequest) (repo.AppInstallation, error) {
	packageLock := m.appLock("package/" + request.PackageIdentity)
	packageLock.Lock()
	defer packageLock.Unlock()
	lock := m.appLock(request.AppID)
	lock.Lock()
	defer lock.Unlock()
	return m.installLocked(ctx, request)
}
func (m *Management) appLock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.installLocks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		m.installLocks[id] = lock
	}
	return lock
}
func (m *Management) installLocked(ctx context.Context, request InstallRequest) (repo.AppInstallation, error) {
	path, err := m.snapshot(ctx, request.FileID)
	if err != nil {
		return repo.AppInstallation{}, err
	}
	defer os.Remove(path)
	bundle, err := InspectBundle(path)
	if err != nil {
		return repo.AppInstallation{}, err
	}
	if bundle.InvalidRoles && !request.SkipInvalidRoles {
		return repo.AppInstallation{}, invalidRequest("Confirm the roles.json warning before importing")
	}
	digest, err := packageDigest(ctx, path)
	if err != nil {
		return repo.AppInstallation{}, err
	}
	if packageIdentity(bundle) != request.PackageIdentity {
		return repo.AppInstallation{}, invalidRequest("App package identity changed after submission")
	}
	target, targetErr := repo.NewAppInstallationRepo(ctx).Get(ctx, request.AppID)
	if targetErr != nil && !errors.Is(targetErr, repo.ErrNotFound) {
		return repo.AppInstallation{}, targetErr
	}
	if targetErr == nil && target.ReleaseID != "" {
		old, err := m.installedBundle(target)
		if err != nil {
			return repo.AppInstallation{}, err
		}
		if packageIdentity(old) != request.PackageIdentity {
			return repo.AppInstallation{}, invalidRequest("replacement package belongs to a different App")
		}
	}
	existing, err := m.matchingInstall(ctx, bundle, digest)
	if err != nil {
		return repo.AppInstallation{}, err
	}
	if existing != nil {
		if existing.AppID != request.AppID {
			return repo.AppInstallation{}, invalidRequest("this App is already installed; use the existing installation")
		}
		if existing.PackageSHA256 == digest && existing.Stage == "ready" {
			if len(request.Collections) > 0 {
				return repo.AppInstallation{}, invalidRequest("existing App grants cannot be changed by a repeated install")
			}
			if len(bundle.Roles) > 0 {
				if err := m.provisionDefinition(ctx, request, bundle); err != nil {
					return repo.AppInstallation{}, err
				}
			}
			return *existing, nil
		}
		if !request.AllowReplace && existing.PackageSHA256 != digest {
			return repo.AppInstallation{}, invalidRequest("this App is already installed; confirm replacement to update it")
		}
	}
	m.metadataMu.Lock()
	err = m.provisionDefinition(ctx, request, bundle)
	m.metadataMu.Unlock()
	if err != nil {
		return repo.AppInstallation{}, err
	}
	if row, e := repo.NewAppInstallationRepo(ctx).Get(ctx, request.AppID); e == nil && row.Stage == "uninstalled" {
		if err = m.setDefinitionEnabled(ctx, request.AppID, true); err != nil {
			return repo.AppInstallation{}, err
		}
	}
	return m.runtime.install(ctx, request.AppID, path, m.credentials.Ensure)
}
func (m *Management) Job(ctx context.Context, id string) (map[string]any, error) {
	subject, _ := contextx.SubjectFrom(ctx)
	job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.JobType != repo.NativeAppInstallJob || job.CreatedBy != subject.UserID {
		return nil, repo.ErrNotFound
	}
	stage := "preparing"
	var request InstallRequest
	if json.Unmarshal(job.RequestJSON, &request) == nil {
		if row, e := repo.NewAppInstallationRepo(ctx).Get(ctx, request.AppID); e == nil && row.OperationID != "" {
			stage = row.Stage
		}
	}
	switch job.Status {
	case repo.AsyncJobStatusPending:
		stage = "queued"
	case repo.AsyncJobStatusSucceeded:
		stage = "ready"
	case repo.AsyncJobStatusFailed:
		stage = "failed"
	}
	return map[string]any{"jobId": job.JobKey, "appId": request.AppID, "stage": stage, "status": job.Status, "progress": job.Progress, "error": job.ErrorMessage, "result": job.ResultJSON}, nil
}
func (m *Management) Action(ctx context.Context, id, action string) error {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" || !contextx.HasResource(subject, "uns.app.manage") {
		return forbiddenRequest("native App management permission required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return invalidRequest("invalid App identity")
	}
	lock := m.appLock(id)
	lock.Lock()
	defer lock.Unlock()
	if _, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app"); err != nil {
		return err
	}
	switch action {
	case "refreshCredentials":
		return m.credentials.Refresh(ctx, id)
	case "start":
		return m.runtime.Start(ctx, id)
	case "uninstall":
		if err := m.runtime.Stop(ctx, id); err != nil {
			return err
		}
		if err := m.credentials.Revoke(ctx, id); err != nil {
			return err
		}
		if err := m.setDefinitionEnabled(ctx, id, false); err != nil {
			return err
		}
		return m.runtime.Uninstall(ctx, id)
	case "stop":
		return m.runtime.Stop(ctx, id)
	case "restart":
		if err := m.runtime.Stop(ctx, id); err != nil {
			return err
		}
		return m.runtime.Start(ctx, id)
	default:
		return invalidRequest("unsupported native App action")
	}
}

func (m *Management) setDefinitionEnabled(ctx context.Context, id string, enabled bool) error {
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, false, "app")
	if err != nil {
		return err
	}
	if row.MountID != nil {
		return forbiddenRequest("mounted Apps cannot be changed by local installation")
	}
	var data unsv2.AppData
	if err = json.Unmarshal(row.Data, &data); err != nil {
		return err
	}
	status := "disabled"
	if enabled {
		status = "active"
	}
	if data.Status == status {
		return nil
	}
	data.Status = status
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = m.uns.Apply(ctx, unsv2.Batch{Mode: "sync", IdempotencyKey: "native-state-" + id + "-" + strconv.FormatInt(row.Revision, 10), Items: []unsv2.Change{{OpID: "app", Action: "meta.replace", Kind: "app", ID: unsv2.Identity{ID: id}, ExpectedRevision: row.Revision, Data: raw}}})
	return err
}
func (m *Management) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.jobs.Wait()
}

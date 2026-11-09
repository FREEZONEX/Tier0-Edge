package repo_test

// 使用真实独立 PG 库验证 HTTP Source 端口事务、并发隔离和路由部署边界；
// 此处不启动 HTTP listener，也不把持久化成功视为网络服务已就绪。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func reserveHTTPTestSource(ctx context.Context, r *repo.SourceStreamRepo, name string, requested, first, last int) (repo.Source, int, error) {
	var source repo.Source
	var allocated int
	err := r.WithHTTPSourcePort(ctx, 0, requested, first, last, func(tx *gorm.DB, port int) error {
		allocated = port
		doc := fmt.Sprintf(`{"protocol":"http-api","role":"server","http":{"port":%d}}`, port)
		var err error
		source, err = repo.NewSourceStreamRepo(tx).CreateSource(ctx, repo.Source{Name: name, Protocol: "http-api", Document: doc})
		return err
	})
	return source, allocated, err
}

func assertHTTPTestPort(t *testing.T, source repo.Source, port int) {
	t.Helper()
	var document struct {
		HTTP struct {
			Port int `json:"port"`
		} `json:"http"`
	}
	require.NoError(t, json.Unmarshal([]byte(source.Document), &document))
	require.Equal(t, port, document.HTTP.Port)
}

func TestHTTPSourcePortReservationLifecycle(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	first, port, err := reserveHTTPTestSource(ctx, r, "first", 0, 31000, 31002)
	require.NoError(t, err)
	require.Equal(t, 31000, port)
	require.Equal(t, "enabled", first.DesiredState)
	_, _, err = reserveHTTPTestSource(ctx, r, "existing-source-conflict", port, 31000, 31002)
	require.ErrorIs(t, err, repo.ErrConflict) // an existing Source keeps its stable reservation

	// A client Source is not a listener and cannot reserve a Server port.
	_, err = r.CreateSource(ctx, repo.Source{Name: "client", Protocol: "http-api", Document: `{"role":"client","http":{"port":31001}}`})
	require.NoError(t, err)
	_, next, err := reserveHTTPTestSource(ctx, r, "second", 0, 31000, 31002)
	require.NoError(t, err)
	require.Equal(t, 31001, next)
	_, next, err = reserveHTTPTestSource(ctx, r, "third", 0, 31000, 31002)
	require.NoError(t, err)
	require.Equal(t, 31002, next)
	_, _, err = reserveHTTPTestSource(ctx, r, "full", 0, 31000, 31002)
	require.ErrorIs(t, err, repo.ErrConflict)

	err = r.WithHTTPSourcePort(ctx, first.ID, port, 31000, 31002, func(tx *gorm.DB, stable int) error {
		require.Equal(t, port, stable)
		var changeErr error
		first, changeErr = repo.NewSourceStreamRepo(tx).ChangeSource(ctx, first.ID, first.Revision, func(_ *gorm.DB, item *repo.Source) error {
			item.Name = "renamed"
			return nil
		})
		return changeErr
	})
	require.NoError(t, err)
	reloaded, err := r.Source(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, reloaded.ID)
	require.Equal(t, "renamed", reloaded.Name)
	assertHTTPTestPort(t, reloaded, 31000)
	_, err = r.ChangeSource(ctx, first.ID, first.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DeletedAt = time.Now().UnixMilli()
		return nil
	})
	require.NoError(t, err)
	_, next, err = reserveHTTPTestSource(ctx, r, "replacement", 0, 31000, 31002)
	require.NoError(t, err)
	require.Equal(t, 31000, next)
}

func TestHTTPSourcePortRollbackAndBounds(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	rolledBack := errors.New("abort source creation")
	var failed repo.Source
	err := r.WithHTTPSourcePort(ctx, 0, 0, 31000, 31015, func(tx *gorm.DB, port int) error {
		var createErr error
		failed, createErr = repo.NewSourceStreamRepo(tx).CreateSource(ctx, repo.Source{Name: "aborted", Protocol: "http-api", Document: fmt.Sprintf(`{"role":"server","http":{"port":%d}}`, port)})
		if createErr != nil {
			return createErr
		}
		return rolledBack
	})
	require.ErrorIs(t, err, rolledBack)
	_, err = r.Source(ctx, failed.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, port, err := reserveHTTPTestSource(ctx, r, "after-rollback", 0, 31000, 31015)
	require.NoError(t, err)
	require.Equal(t, 31000, port)
	for _, limits := range [][3]int{{0, 1023, 1024}, {0, 31002, 31001}, {0, 65000, 65536}, {0, 31000, 31256}, {30999, 31000, 31015}, {31016, 31000, 31015}, {-1, 31000, 31015}} {
		called := false
		err := r.WithHTTPSourcePort(ctx, 0, limits[0], limits[1], limits[2], func(*gorm.DB, int) error { called = true; return nil })
		require.ErrorIs(t, err, repo.ErrInvalidArgument, "limits=%v", limits)
		require.False(t, called)
	}
	// The route guard provides the same transaction-local rollback boundary.
	var guardSource repo.Source
	err = r.HTTPRouteGuard(ctx, func(tx *gorm.DB) error {
		var createErr error
		guardSource, createErr = repo.NewSourceStreamRepo(tx).CreateSource(ctx, repo.Source{Name: "guard-aborted", Protocol: "http-api", Document: `{"role":"client"}`})
		if createErr != nil {
			return createErr
		}
		return rolledBack
	})
	require.ErrorIs(t, err, rolledBack)
	_, err = r.Source(ctx, guardSource.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	called := false
	require.NoError(t, r.HTTPRouteGuard(ctx, func(tx *gorm.DB) error { called = true; return tx.Exec("SELECT 1").Error }))
	require.True(t, called)
}

func TestHTTPSourcePortConcurrentReservation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	const count = 8
	type result struct {
		source repo.Source
		port   int
		err    error
	}
	results := make(chan result, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index := 0; index < count; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			source, port, err := reserveHTTPTestSource(ctx, r, fmt.Sprintf("concurrent-%d", index), 0, 31000, 31007)
			results <- result{source, port, err}
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)
	seenPorts, seenIDs := map[int]bool{}, map[int64]bool{}
	for result := range results {
		require.NoError(t, result.err)
		require.False(t, seenPorts[result.port])
		require.False(t, seenIDs[result.source.ID])
		seenPorts[result.port], seenIDs[result.source.ID] = true, true
		loaded, err := r.Source(ctx, result.source.ID)
		require.NoError(t, err)
		assertHTTPTestPort(t, loaded, result.port)
	}
	require.Len(t, seenPorts, count)
	for port := 31000; port <= 31007; port++ {
		require.True(t, seenPorts[port])
	}
	_, _, err := reserveHTTPTestSource(ctx, r, "concurrent-full", 0, 31000, 31007)
	require.ErrorIs(t, err, repo.ErrConflict)
}

func TestHTTPRoutesOverlap(t *testing.T) {
	entry := func(source, method, path string) streamexec.ProgramEntry {
		return streamexec.ProgramEntry{Kind: "http-request", SourceID: source, Method: method, Path: path}
	}
	for _, item := range []struct {
		left, right streamexec.ProgramEntry
		overlap     bool
	}{
		{entry("1", "GET", "/orders/:id"), entry("1", "GET", "/orders/current"), true},
		{entry("1", "GET", "/orders/:id"), entry("1", "GET", "/orders/:order"), true},
		{entry("1", "POST", "/"), entry("1", "POST", "/"), true},
		{entry("1", "GET", "/orders/:id"), entry("2", "GET", "/orders/current"), false},
		{entry("1", "GET", "/orders/:id"), entry("1", "POST", "/orders/current"), false},
		{entry("1", "GET", "/orders/:id"), entry("1", "GET", "/orders/:id/items"), false},
		{entry("1", "GET", "/orders/new"), entry("1", "GET", "/orders/current"), false},
	} {
		require.Equal(t, item.overlap, repo.HTTPRoutesOverlap(item.left, item.right))
		require.Equal(t, item.overlap, repo.HTTPRoutesOverlap(item.right, item.left))
	}
}

func httpRepoTestManifest(sourceID int64, method, path string) streamexec.ProgramManifest {
	id := strconv.FormatInt(sourceID, 10)
	return streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{{ID: id, Resources: []string{}}}, Topics: []streamexec.ProgramTopic{}, Entries: []streamexec.ProgramEntry{{ID: "http", Kind: "http-request", SourceID: id, Method: method, Path: path}}}
}

func httpRepoTestSnapshots(source repo.Source) []repo.StreamSourceSnapshot {
	return []repo.StreamSourceSnapshot{{Alias: strconv.FormatInt(source.ID, 10), SourceID: source.ID, ConfigRevision: source.ConfigRevision, Resources: []string{}}}
}

func httpRepoTestReadySource(t *testing.T, r *repo.SourceStreamRepo) repo.Source {
	t.Helper()
	source, _, err := reserveHTTPTestSource(context.Background(), r, "http-server", 0, 31000, 31015)
	require.NoError(t, err)
	source, err = r.ChangeSource(context.Background(), source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = "enabled"
		item.Health = "healthy"
		return nil
	})
	require.NoError(t, err)
	return source
}

func TestHTTPProgramManifestValidation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	source := httpRepoTestReadySource(t, r)
	ctx := context.Background()
	doc := `{"script":"valid"}`
	valid := httpRepoTestManifest(source.ID, "GET", "/orders/:id")
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "valid", Model: "program", Document: doc}, 0, valid)
	require.NoError(t, err)
	loaded, err := stream.ProgramV2DraftManifest()
	require.NoError(t, err)
	require.Equal(t, valid, loaded)
	for name, change := range map[string]func(*streamexec.ProgramManifest){
		"unknown_source":        func(m *streamexec.ProgramManifest) { m.Entries[0].SourceID = "0" },
		"undeclared_source":     func(m *streamexec.ProgramManifest) { m.Sources = []streamexec.ProgramSource{} },
		"missing_resources":     func(m *streamexec.ProgramManifest) { m.Sources[0].Resources = nil },
		"invalid_method":        func(m *streamexec.ProgramManifest) { m.Entries[0].Method = "get" },
		"invalid_path":          func(m *streamexec.ProgramManifest) { m.Entries[0].Path = "/orders/%2e%2e" },
		"duplicate_parameter":   func(m *streamexec.ProgramManifest) { m.Entries[0].Path = "/:id/:id" },
		"extra_resource":        func(m *streamexec.ProgramManifest) { m.Entries[0].ResourceRefs = []string{"op"} },
		"extra_topic":           func(m *streamexec.ProgramManifest) { m.Entries[0].TopicIDs = []string{"1"} },
		"extra_qos":             func(m *streamexec.ProgramManifest) { m.Entries[0].QoS = 1 },
		"extra_interval":        func(m *streamexec.ProgramManifest) { m.Entries[0].IntervalMs = 100 },
		"non_http_route_fields": func(m *streamexec.ProgramManifest) { m.Entries[0].Kind = "request"; m.Entries[0].SourceID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := httpRepoTestManifest(source.ID, "GET", "/orders/:id")
			change(&manifest)
			_, err := r.SaveProgramV2(ctx, repo.Stream{Name: name, Model: "program", Document: doc}, 0, manifest)
			require.ErrorIs(t, err, repo.ErrInvalidArgument)
		})
	}
	// Old source-less request entries retain their independent contract.
	_, err = r.SaveProgramV2(ctx, repo.Stream{Name: "source-less", Model: "program", Document: doc}, 0, v2RequestManifest())
	require.NoError(t, err)
}

func TestHTTPProgramRouteDeploymentConflictsAndSelfReplacement(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	source := httpRepoTestReadySource(t, r)
	ctx := context.Background()
	snapshots := httpRepoTestSnapshots(source)
	doc := `{"script":"first"}`
	save := func(name, path string) (repo.Stream, streamexec.ProgramManifest) {
		manifest := httpRepoTestManifest(source.ID, "GET", path)
		stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: name, Model: "program", Document: doc}, 0, manifest)
		require.NoError(t, err)
		return stream, manifest
	}
	active, activeManifest := save("active", "/orders/:id")
	active, original, err := r.StageProgramV2(ctx, active.ID, active.Revision, 0, doc, activeManifest, snapshots)
	require.NoError(t, err)
	active, err = r.ActivateStagedProgramV2(ctx, active.ID, active.Revision, original.ID)
	require.NoError(t, err)
	blocked, blockedManifest := save("blocked-active", "/orders/current")
	_, _, err = r.StageProgramV2(ctx, blocked.ID, blocked.Revision, 0, doc, blockedManifest, snapshots)
	require.ErrorIs(t, err, repo.ErrConflict)
	blocked, err = r.Stream(ctx, blocked.ID)
	require.NoError(t, err)
	require.Zero(t, blocked.PendingVersion)

	candidate, candidateManifest := save("candidate", "/pending/:id")
	candidate, _, err = r.StageProgramV2(ctx, candidate.ID, candidate.Revision, 0, doc, candidateManifest, snapshots)
	require.NoError(t, err)
	require.Zero(t, candidate.ActiveVersion)
	require.Positive(t, candidate.PendingVersion)
	pendingBlocked, pendingManifest := save("blocked-candidate", "/pending/current")
	_, _, err = r.StageProgramV2(ctx, pendingBlocked.ID, pendingBlocked.Revision, 0, doc, pendingManifest, snapshots)
	require.ErrorIs(t, err, repo.ErrConflict)

	// A new version replaces its own route, rather than conflicting with it.
	active.Document = `{"script":"second"}`
	active, err = r.SaveProgramV2(ctx, active, active.Revision, activeManifest)
	require.NoError(t, err)
	active, replacement, err := r.StageProgramV2(ctx, active.ID, active.Revision, 0, active.Document, activeManifest, snapshots)
	require.NoError(t, err)
	active, err = r.ActivateStagedProgramV2(ctx, active.ID, active.Revision, replacement.ID)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, active.ActiveVersion)
	active, rollback, err := r.StageProgramV2(ctx, active.ID, active.Revision, original.ID, doc, activeManifest, snapshots)
	require.NoError(t, err)
	active, err = r.ActivateStagedProgramV2(ctx, active.ID, active.Revision, rollback.ID)
	require.NoError(t, err)
	require.Equal(t, original.ID, active.ActiveVersion)

	// Ambiguous routes inside one manifest fail at deployment, not just runtime.
	duplicate, duplicateManifest := save("ambiguous-own", "/duplicates/:id")
	duplicateManifest.Entries = append(duplicateManifest.Entries, streamexec.ProgramEntry{ID: "other", Kind: "http-request", SourceID: strconv.FormatInt(source.ID, 10), Method: "GET", Path: "/duplicates/current"})
	duplicate, err = r.SaveProgramV2(ctx, duplicate, duplicate.Revision, duplicateManifest)
	require.NoError(t, err)
	_, _, err = r.StageProgramV2(ctx, duplicate.ID, duplicate.Revision, 0, doc, duplicateManifest, snapshots)
	require.ErrorIs(t, err, repo.ErrConflict)

	_, err = r.DisableStream(ctx, candidate.ID, candidate.Revision, false)
	require.NoError(t, err)
	pendingBlocked, version, err := r.StageProgramV2(ctx, pendingBlocked.ID, pendingBlocked.Revision, 0, doc, pendingManifest, snapshots)
	require.NoError(t, err)
	_, err = r.ActivateStagedProgramV2(ctx, pendingBlocked.ID, pendingBlocked.Revision, version.ID)
	require.NoError(t, err)
	// Route manifests remain genuine typed data, not a scanned script string.
	encoded, err := json.Marshal(activeManifest)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"kind":"http-request"`)
}

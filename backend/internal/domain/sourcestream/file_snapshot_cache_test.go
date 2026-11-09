package sourcestream

// 使用真实限速 HTTP 与 SQLite 文件验证后台下载、强校验续传、并发预算和取消。
// 夹具监听本机私网地址，以保留生产拨号器对环回、链路本地和元数据地址的拒绝。
import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcehealth"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func fileSnapshotHTTPServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	for _, addr := range addrs {
		prefix, err := netip.ParsePrefix(addr.String())
		if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() || !httpSourceAddressAllowed(prefix.Addr().String(), prefix.Addr()) {
			continue
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort(prefix.Addr().String(), "0"))
		if err != nil {
			continue
		}
		server := httptest.NewUnstartedServer(handler)
		server.Listener.Close()
		server.Listener = listener
		server.Start()
		t.Cleanup(server.Close)
		return server
	}
	t.Fatal("真实文件传输测试需要一个可监听的本机 IPv4 私网接口")
	return nil
}

func fileSnapshotTestDefinition(server *httptest.Server) SourceDefinition {
	return SourceDefinition{Protocol: FileRead, Endpoint: server.URL, File: &FileSourceOptions{Resources: []FileSelection{{ID: "qc", Path: "snapshot.sqlite", Tables: []string{"T_YHB"}}}}}
}

func fileSnapshotWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("文件工作任务未在测试预算内结束")
	}
}

func fileSnapshotCleanup(t *testing.T, key fileSnapshotKey) {
	t.Helper()
	t.Cleanup(func() {
		fileSnapshots.Lock()
		snapshot := fileSnapshots.entries[key]
		if snapshot != nil {
			require.Zero(t, snapshot.leases)
			evictFileSnapshot(key, snapshot)
		}
		fileSnapshots.Unlock()
		if snapshot != nil && snapshot.http != nil {
			fileSnapshotWait(t, snapshot.http.done)
		}
	})
}

func TestFileSnapshotHTTPContinuesAfterCallerDeadlineAndReadsSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qc.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE T_YHB(YHM TEXT, XM TEXT, BM TEXT, QX TEXT); INSERT INTO T_YHB VALUES ('qa-001','Inspector','Quality','read');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	var full atomic.Int64
	var continued atomic.Int64
	gate := make(chan struct{})
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"stable-qc"`)
		if r.Header.Get("If-None-Match") == `"stable-qc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		full.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:512])
		w.(http.Flusher).Flush()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		n, _ := w.Write(body[512:])
		continued.Add(int64(n))
	})
	def := fileSnapshotTestDefinition(server)
	row := repo.Source{ID: 772001, ConfigRevision: 1}
	key := fileSnapshotKey{row.ID, row.ConfigRevision, "qc"}
	fileSnapshotCleanup(t, key)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	service := &Service{}
	started := time.Now()
	nativeResult, err := service.programNativePage(ctx, row, def, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "qc2000:qc:T_YHB", Options: []byte(`{"limit":1,"timeoutMs":10000}`)}, true)
	require.Nil(t, nativeResult, "未完成下载不能伪装成成功空页")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, ctx.Err(), "原生读取自己的 10 秒期限先到；不能误用上层取消终止后台任务")
	require.GreaterOrEqual(t, time.Since(started), 10*time.Second)
	var pending *sourcehealth.Failure
	require.ErrorAs(t, err, &pending)
	require.Equal(t, "SOURCE_SNAPSHOT_PENDING", pending.Code)
	require.Contains(t, pending.Message, "receivedBytes=512")
	require.Contains(t, pending.Message, fmt.Sprintf("totalBytes=%d", len(body)))
	require.NotContains(t, pending.Message, server.URL)
	snapshot, release, err := acquireFileSnapshot(context.Background(), key)
	require.NoError(t, err)
	job := snapshot.http
	require.NotNil(t, job)
	release()
	close(gate)
	fileSnapshotWait(t, job.done)
	require.EqualValues(t, len(body)-512, continued.Load(), "短请求结束后继续下载，无需下一次 timer")
	result, err := service.fileResourcePage(context.Background(), row, def, "qc2000:qc:T_YHB", "", 1, 0, true)
	require.NoError(t, err)
	page := result.(map[string]any)
	items := page["items"].([]map[string]any)
	require.Len(t, items, 1)
	require.Equal(t, "qa-001", items[0]["username"])
	require.Equal(t, fileDigest(body), items[0]["fileSha256"])
	require.EqualValues(t, 1, full.Load(), "完整文件仅下载一次")
	_, err = service.fileResourcePage(context.Background(), row, def, "qc2000:qc:T_YHB", "", 1, 0, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, full.Load(), "稳定 ETag 复用已验证快照")
}

func TestFileSnapshotHTTPUpdatesValidatorWhenContentIsUnchanged(t *testing.T) {
	var generation atomic.Int64
	generation.Store(1)
	var full atomic.Int64
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		etag := fmt.Sprintf(`"v%d"`, generation.Load())
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		full.Add(1)
		_, _ = io.WriteString(w, "unchanged-content")
	})
	def := fileSnapshotTestDefinition(server)
	snapshot := &fileSnapshot{}
	t.Cleanup(func() {
		if snapshot.file != nil {
			snapshot.file.Close()
			os.Remove(snapshot.file.Name())
		}
	})
	require.NoError(t, snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, ""))
	installed := snapshot.file
	require.Equal(t, `"v1"`, snapshot.etag)
	generation.Store(2)
	require.NoError(t, snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, ""))
	require.Same(t, installed, snapshot.file, "内容未变时保留已验证文件")
	require.Equal(t, `"v2"`, snapshot.etag, "同内容的新强校验值也必须更新")
	require.EqualValues(t, 2, full.Load())
	require.NoError(t, snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, ""))
	require.EqualValues(t, 2, full.Load(), "后续使用新 ETag 获得 304，不重复完整下载")
}

func TestFileSnapshotHTTPBackgroundDeadlinePreservesResume(t *testing.T) {
	var requests atomic.Int64
	body := []byte("bounded-continuous-download")
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"bounded-v1"`)
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body[:4])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Header.Get("If-Match") != `"bounded-v1"` || r.Header.Get("Range") != "bytes=4-" {
			t.Error("工作预算结束后没有使用同版本精确续传")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(body)-1, len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-4))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[4:])
	})
	def := fileSnapshotTestDefinition(server)
	snapshot := &fileSnapshot{}
	t.Cleanup(func() {
		if snapshot.http != nil {
			snapshot.http.cancel()
			fileSnapshotWait(t, snapshot.http.done)
		}
		if snapshot.file != nil {
			snapshot.file.Close()
			os.Remove(snapshot.file.Name())
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	job := snapshot.http
	require.NotNil(t, job)
	select {
	case <-job.done:
	case <-time.After(95 * time.Second):
		t.Fatal("真实 90 秒后台预算未结束")
	}
	require.GreaterOrEqual(t, time.Since(started), 90*time.Second)
	require.ErrorIs(t, job.err, context.DeadlineExceeded)
	require.EqualValues(t, 1, requests.Load(), "后台任务不会在预算结束后自行无限重试")
	require.True(t, job.budgetExpired)
	require.Equal(t, string(body[:4]), job.snapshot.partial.String())
	// 下一次有效读取直接续传，不必先消耗一次请求接收旧任务的超时。
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, ""))
	require.Equal(t, fileDigest(body), snapshot.version)
	require.EqualValues(t, 2, requests.Load())
}

func TestFileSnapshotHTTPResumeExpiredWorkerBoundaries(t *testing.T) {
	for _, scenario := range []string{"expired", "upstream-timeout", "cancelled", "weak-etag", "no-progress", "network-error", "caller-expired"} {
		t.Run(scenario, func(t *testing.T) {
			body := []byte("verified-snapshot")
			var requests atomic.Int64
			server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("ETag", `"v1"`)
				if r.Header.Get("If-None-Match") == `"v1"` {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				if r.Header.Get("If-Match") != `"v1"` || r.Header.Get("Range") != "bytes=4-" {
					t.Error("只允许同一强 ETag 和精确断点续传")
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(body)-1, len(body)))
				w.Header().Set("Content-Length", strconv.Itoa(len(body)-4))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(body[4:])
			})
			def := fileSnapshotTestDefinition(server)
			// 预置已经结束的任务；真实 90 秒预算由上面的集成用例覆盖。
			job := &fileHTTPTransferJob{done: make(chan struct{}), cancel: func() {}, handled: true, err: context.DeadlineExceeded, budgetExpired: true,
				snapshot: &fileSnapshot{partial: *bytes.NewBuffer(bytes.Clone(body[:4])), partialETag: `"v1"`, total: int64(len(body))}}
			close(job.done)
			ctx := context.Background()
			wantErr := error(context.DeadlineExceeded)
			switch scenario {
			case "upstream-timeout":
				job.budgetExpired = false
			case "cancelled":
				job.budgetExpired, job.err, wantErr = false, context.Canceled, context.Canceled
			case "weak-etag":
				job.snapshot.partialETag = `W/"v1"`
			case "no-progress":
				job.snapshot.partial.Reset()
			case "network-error":
				job.err, wantErr = io.ErrUnexpectedEOF, io.ErrUnexpectedEOF
			case "caller-expired":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			snapshot := &fileSnapshot{http: job}
			t.Cleanup(func() {
				if snapshot.file != nil {
					snapshot.file.Close()
					os.Remove(snapshot.file.Name())
				}
			})
			err := snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
			if scenario == "expired" {
				require.NoError(t, err)
				require.EqualValues(t, 2, requests.Load(), "续传后仍须第二次强校验")
				require.Equal(t, fileDigest(body), snapshot.version)
			} else {
				require.ErrorIs(t, err, wantErr)
				require.Zero(t, requests.Load(), "不将其他失败自动当作可续接的预算到期")
			}
		})
	}
}

func TestFileSnapshotHTTPResumedWorkerKeepsCallerDeadlineAndCancellation(t *testing.T) {
	body := []byte("bounded-resumed-download")
	var requests atomic.Int64
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-Match") != `"v1"` || r.Header.Get("Range") != "bytes=4-" {
			t.Error("续接必须保留原断点和强校验")
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(body)-1, len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-4))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[4:8])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	def := fileSnapshotTestDefinition(server)
	old := &fileHTTPTransferJob{done: make(chan struct{}), cancel: func() {}, handled: true, err: context.DeadlineExceeded, budgetExpired: true,
		snapshot: &fileSnapshot{partial: *bytes.NewBuffer(bytes.Clone(body[:4])), partialETag: `"v1"`, total: int64(len(body))}}
	close(old.done)
	snapshot := &fileSnapshot{http: old}
	t.Cleanup(func() {
		if snapshot.http != nil {
			snapshot.http.cancel()
			fileSnapshotWait(t, snapshot.http.done)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
	var pending *sourcehealth.Failure
	require.ErrorAs(t, err, &pending)
	require.Equal(t, "SOURCE_SNAPSHOT_PENDING", pending.Code)
	require.Contains(t, pending.Message, "receivedBytes=8")
	require.Contains(t, pending.Message, fmt.Sprintf("totalBytes=%d", len(body)))
	job := snapshot.http
	require.NotSame(t, old, job)
	select {
	case <-job.done:
		t.Fatal("续接的任务不能随调用方短截止时间终止")
	default:
	}
	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	err = snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
	require.ErrorIs(t, err, context.Canceled)
	fileSnapshotWait(t, job.done)
	require.ErrorIs(t, job.err, context.Canceled)
	require.False(t, job.budgetExpired)
	require.EqualValues(t, 1, requests.Load(), "显式取消后不再续接")
	require.Empty(t, fileTransferWorkers, "续接任务结束后释放共享并发额度")
}

func TestFileSnapshotHTTPResumesOnlyExactStrongValidatorRange(t *testing.T) {
	for _, scenario := range []string{"resume", "etag-changed", "bad-range", "ignored-range", "cursor-changed"} {
		t.Run(scenario, func(t *testing.T) {
			body := []byte("stable-content")
			var requests atomic.Int64
			server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"v1"`)
				if r.Header.Get("If-None-Match") != "" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				if requests.Add(1) == 1 {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
					_, _ = w.Write(body[:4])
					return // 提前 EOF，下一次必须从相同强 ETag 的精确偏移续传。
				}
				if r.Header.Get("If-Match") != `"v1"` || r.Header.Get("Range") != "bytes=4-" {
					t.Error("续传未携带冻结 ETag 或精确偏移")
				}
				if scenario == "etag-changed" {
					w.Header().Set("ETag", `"v2"`)
				}
				if scenario == "ignored-range" {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
					_, _ = w.Write(body)
					return
				}
				end := len(body) - 1
				if scenario == "bad-range" {
					end--
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", end, len(body)))
				w.Header().Set("Content-Length", strconv.Itoa(len(body)-4))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(body[4:])
			})
			def := fileSnapshotTestDefinition(server)
			snapshot := &fileSnapshot{}
			t.Cleanup(func() {
				if snapshot.file != nil {
					snapshot.file.Close()
					os.Remove(snapshot.file.Name())
				}
			})
			err := snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, "")
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			require.Equal(t, "stab", snapshot.partial.String())
			expected := ""
			if scenario == "cursor-changed" {
				expected = strings.Repeat("0", 64)
			}
			err = snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, expected)
			if scenario == "etag-changed" || scenario == "bad-range" || scenario == "cursor-changed" {
				var invalid *ValidationError
				require.ErrorAs(t, err, &invalid)
				require.Nil(t, snapshot.file)
				require.Empty(t, snapshot.partial.Bytes(), "不同版本或错误范围不得保留可拼接字节")
				return
			}
			require.NoError(t, err)
			require.Equal(t, fileDigest(body), snapshot.version)
			actual, err := os.ReadFile(snapshot.file.Name())
			require.NoError(t, err)
			require.Equal(t, body, actual)
		})
	}
}

func TestFileSnapshotHTTPWeakValidatorStillRequiresTwoMatchingReads(t *testing.T) {
	var requests atomic.Int64
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Range"))
		requests.Add(1)
		w.Header().Set("ETag", `W/"weak"`)
		_, _ = io.WriteString(w, "same-file")
	})
	def := fileSnapshotTestDefinition(server)
	snapshot := &fileSnapshot{}
	require.NoError(t, snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, ""))
	t.Cleanup(func() { snapshot.file.Close(); os.Remove(snapshot.file.Name()) })
	require.EqualValues(t, 3, requests.Load(), "一次能力探测后仍须两次完整读取")
	require.Equal(t, fileDigest([]byte("same-file")), snapshot.version)
	require.Empty(t, snapshot.etag)
}

func TestFileSnapshotHTTPRejectsUnstableOrUnboundedResponses(t *testing.T) {
	for _, scenario := range []string{"too-large", "encoded", "unsolicited-range", "changed-at-confirm", "invalid-304"} {
		t.Run(scenario, func(t *testing.T) {
			server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"v1"`)
				if scenario == "invalid-304" {
					w.Header().Set("ETag", `"other"`)
					w.WriteHeader(http.StatusNotModified)
					return
				}
				if scenario == "changed-at-confirm" && r.Header.Get("If-None-Match") != "" {
					w.Header().Set("ETag", `"v2"`)
				}
				if scenario == "too-large" {
					w.Header().Set("Content-Length", strconv.Itoa(maxSourceFileBytes+1))
				} else {
					w.Header().Set("Content-Length", "4")
				}
				if scenario == "encoded" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				if scenario == "unsolicited-range" {
					w.WriteHeader(http.StatusPartialContent)
				}
				_, _ = io.WriteString(w, "body")
			})
			def := fileSnapshotTestDefinition(server)
			snapshot := &fileSnapshot{}
			if scenario == "invalid-304" {
				require.NoError(t, snapshot.installBytes([]byte("old"), fileDigest([]byte("old")), `"v1"`))
				t.Cleanup(func() { snapshot.file.Close(); os.Remove(snapshot.file.Name()) })
			}
			err := snapshot.prepare(context.Background(), def, def.File.Resources[0], Credential{}, "")
			var invalid *ValidationError
			require.ErrorAs(t, err, &invalid)
			want := map[string]string{"too-large": "tooLarge", "encoded": "unsupportedEncoding", "unsolicited-range": "invalidRange", "changed-at-confirm": "writeInProgress", "invalid-304": "invalidValidator"}[scenario]
			require.Equal(t, want, invalid.Code)
			if scenario == "invalid-304" {
				data, err := os.ReadFile(snapshot.file.Name())
				require.NoError(t, err)
				require.Equal(t, "old", string(data), "失败的验证不能替换原稳定快照")
			} else {
				require.Nil(t, snapshot.file, "不稳定或超界响应不能安装为可读快照")
			}
		})
	}
}

func TestFileSnapshotHTTPBudgetCancellationAndRevisionIsolation(t *testing.T) {
	var active atomic.Int64
	server := fileSnapshotHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		w.Header().Set("ETag", `"blocked"`)
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write(bytes.Repeat([]byte{'x'}, 4))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	def := fileSnapshotTestDefinition(server)
	var jobs []*fileHTTPTransferJob
	for i := 0; i < cap(fileTransferWorkers); i++ {
		key := fileSnapshotKey{772010 + int64(i), 1, "qc"}
		fileSnapshotCleanup(t, key)
		snapshot, release, err := acquireFileSnapshot(context.Background(), key)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err = snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		jobs = append(jobs, snapshot.http)
		release()
	}
	require.Equal(t, cap(fileTransferWorkers), len(fileTransferWorkers))
	_, err := (&fileSnapshot{}).prepareHTTPTransfer(context.Background(), def, def.File.Resources[0], Credential{}, "")
	require.ErrorIs(t, err, ErrUnavailable, "共享全局预算，不为强 ETag 新开无限并发")
	// 相同版本复用已有工作任务，取消当前等待也取消该工作任务。
	key := fileSnapshotKey{772010, 1, "qc"}
	snapshot, release, err := acquireFileSnapshot(context.Background(), key)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err = snapshot.prepare(ctx, def, def.File.Resources[0], Credential{}, "")
	require.ErrorIs(t, err, context.Canceled)
	require.Same(t, jobs[0], snapshot.http)
	release()
	fileSnapshotWait(t, jobs[0].done)
	require.ErrorIs(t, jobs[0].err, context.Canceled)
	// 新配置版本淘汰旧缓存，同时取消尚未完成的旧版本下载。
	newKey := fileSnapshotKey{772011, 2, "qc"}
	fileSnapshotCleanup(t, newKey)
	newSnapshot, release, err := acquireFileSnapshot(context.Background(), newKey)
	require.NoError(t, err)
	require.Nil(t, newSnapshot.http)
	require.Empty(t, newSnapshot.partial.Bytes())
	release()
	fileSnapshotWait(t, jobs[1].done)
	require.True(t, errors.Is(jobs[1].err, context.Canceled))
	for _, job := range jobs[2:] {
		job.cancel()
		fileSnapshotWait(t, job.done)
	}
	require.Eventually(t, func() bool { return active.Load() == 0 && len(fileTransferWorkers) == 0 }, time.Second, 5*time.Millisecond)
}

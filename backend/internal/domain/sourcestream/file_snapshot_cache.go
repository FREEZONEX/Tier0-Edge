package sourcestream

import (
	"backend/internal/domain/sourcehealth"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// One immutable local file per Source revision and selection. Serializing its
// short-lived leases also prevents two retries from appending to the same transfer.
type fileSnapshotKey struct {
	source, revision int64
	selection        string
}
type fileSnapshot struct {
	directoryJob     *directoryListJob
	directoryNames   []string
	directoryVersion string
	native           *fileTransferJob
	http             *fileHTTPTransferJob
	lock             chan struct{}
	used             time.Time
	leases           int
	expiry           *time.Timer
	file             *os.File
	version, etag    string
	checked          map[string]bool
	ediOriginals     map[string]string
	partial          bytes.Buffer
	partialETag      string
	total            int64
	progress         *fileHTTPTransferJob
}

var fileSnapshots = struct {
	sync.Mutex
	entries map[fileSnapshotKey]*fileSnapshot
}{entries: map[fileSnapshotKey]*fileSnapshot{}}

const fileSnapshotTTL = 10 * time.Minute

func evictFileSnapshot(key fileSnapshotKey, s *fileSnapshot) {
	if s.directoryJob != nil {
		s.directoryJob.cancel()
	}
	if s.native != nil {
		s.native.cancel()
	}
	if s.http != nil {
		s.http.cancel()
	}
	delete(fileSnapshots.entries, key)
	if s.expiry != nil {
		s.expiry.Stop()
	}
	if s.file != nil {
		s.file.Close()
		os.Remove(s.file.Name())
	}
}

func acquireFileSnapshot(ctx context.Context, key fileSnapshotKey) (*fileSnapshot, func(), error) {
	fileSnapshots.Lock()
	for old, v := range fileSnapshots.entries {
		if old.source == key.source && old.revision != key.revision && v.leases == 0 {
			evictFileSnapshot(old, v)
		}
	}
	s := fileSnapshots.entries[key]
	if s == nil {
		if len(fileSnapshots.entries) >= 8 {
			var oldest *fileSnapshot
			var victim fileSnapshotKey
			for k, v := range fileSnapshots.entries {
				if v.leases == 0 && (oldest == nil || v.used.Before(oldest.used)) {
					oldest, victim = v, k
				}
			}
			if oldest == nil {
				fileSnapshots.Unlock()
				return nil, nil, ErrUnavailable
			}
			evictFileSnapshot(victim, oldest)
		}
		s = &fileSnapshot{lock: make(chan struct{}, 1)}
		fileSnapshots.entries[key] = s
	}
	if s.expiry != nil {
		s.expiry.Stop()
	}
	s.leases++
	fileSnapshots.Unlock()
	release := func() {
		fileSnapshots.Lock()
		defer fileSnapshots.Unlock()
		s.leases--
		s.used = time.Now()
		if s.leases == 0 {
			s.expiry = time.AfterFunc(fileSnapshotTTL, func() {
				fileSnapshots.Lock()
				defer fileSnapshots.Unlock()
				if fileSnapshots.entries[key] == s && s.leases == 0 && time.Since(s.used) >= fileSnapshotTTL {
					evictFileSnapshot(key, s)
				}
			})
		}
	}
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case s.lock <- struct{}{}:
		return s, func() { <-s.lock; release() }, nil
	}
}

func (s *fileSnapshot) install(f *os.File, version, etag string) {
	if s.file != nil {
		s.file.Close()
		os.Remove(s.file.Name())
	}
	s.file, s.version, s.etag = f, version, etag
	s.checked = map[string]bool{}
	s.ediOriginals = nil
}

func (s *fileSnapshot) prepare(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) error {
	if expected != "" && expected == s.version && s.file != nil {
		return nil
	}
	if s.native != nil {
		return s.prepareUnversioned(ctx, d, r, c, expected)
	}
	if strings.HasPrefix(d.Endpoint, "http://") || strings.HasPrefix(d.Endpoint, "https://") {
		handled, e := s.prepareHTTPTransfer(ctx, d, r, c, expected)
		if handled || e != nil {
			return e
		}
	}
	return s.prepareUnversioned(ctx, d, r, c, expected)
}

// Some servers offer no stable validator and need two complete remote reads.
// Keep one bounded observation alive across the short cloud request deadline.
// The worker owns all transport/temp handles and never writes into the cache.
type fileTransferJob struct {
	done    chan struct{}
	cancel  context.CancelFunc
	bytes   []byte
	version string
	err     error
}

var fileTransferWorkers = make(chan struct{}, 4)

// HTTP 工作任务独占暂存字节和临时文件；调用方超时不打断下载，不能让工作协程修改共享快照。
// 与无版本文件共享并发和时间预算，只有强 ETag 的未完成内容可以跨任务续传。
type fileHTTPTransferJob struct {
	done          chan struct{}
	cancel        context.CancelFunc
	snapshot      *fileSnapshot
	bytes         []byte
	handled       bool
	err           error
	budgetExpired bool
	received      atomic.Int64
	total         atomic.Int64
}

func (s *fileSnapshot) prepareHTTPTransfer(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) (bool, error) {
	resumed := false
	for {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if s.http == nil {
			select {
			case fileTransferWorkers <- struct{}{}:
			default:
				return true, ErrUnavailable
			}
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
			job := &fileHTTPTransferJob{done: make(chan struct{}), cancel: cancel}
			job.snapshot = &fileSnapshot{version: s.version, etag: s.etag, partial: s.partial, partialETag: s.partialETag, total: s.total, progress: job}
			job.received.Store(int64(s.partial.Len()))
			job.total.Store(s.total)
			s.forgetPartial()
			s.http = job
			go func() {
				defer func() { cancel(); <-fileTransferWorkers; close(job.done) }()
				job.handled, job.err = job.snapshot.prepareHTTP(work, d, r, c, "")
				job.budgetExpired = errors.Is(work.Err(), context.DeadlineExceeded)
				if f := job.snapshot.file; f != nil {
					defer os.Remove(f.Name())
					defer f.Close()
					job.bytes, job.err = io.ReadAll(io.NewSectionReader(f, 0, maxSourceFileBytes+1))
				}
			}()
		}
		job := s.http
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				job.cancel()
				return true, ctx.Err()
			}
			return true, sourcehealth.Fail("download", "SOURCE_SNAPSHOT_PENDING",
				fmt.Sprintf("Snapshot download is pending: receivedBytes=%d, totalBytes=%d; retry the same Source configuration and resource", job.received.Load(), job.total.Load()), ctx.Err())
		case <-job.done:
		}
		s.http = nil
		staged := job.snapshot
		s.partial, s.partialETag, s.total = staged.partial, staged.partialETag, staged.total
		if job.err != nil || !job.handled {
			// 只有工作预算到期且保留强校验断点时，当前获准读取才续接一次；其他失败仍交给调用方。
			if !resumed && job.handled && job.budgetExpired && errors.Is(job.err, context.DeadlineExceeded) &&
				strongFileETag(s.partialETag) && s.partial.Len() > 0 {
				resumed = true
				continue
			}
			return job.handled, job.err
		}
		if expected != "" && staged.version != expected {
			return true, invalid("cursor", "fileVersionChanged")
		}
		if staged.version == s.version && s.file != nil {
			s.etag = staged.etag
			return true, nil
		}
		return true, s.installBytes(job.bytes, staged.version, staged.etag)
	}
}

type fileSnapshotProgressWriter struct {
	snapshot *fileSnapshot
}

func (w fileSnapshotProgressWriter) Write(p []byte) (int, error) {
	n, err := w.snapshot.partial.Write(p)
	if progress := w.snapshot.progress; progress != nil {
		progress.received.Add(int64(n))
	}
	return n, err
}

func (s *fileSnapshot) prepareUnversioned(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) error {
	if s.native == nil {
		select {
		case fileTransferWorkers <- struct{}{}:
		default:
			return ErrUnavailable
		}
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		job := &fileTransferJob{done: make(chan struct{}), cancel: cancel}
		s.native = job
		go func() {
			defer func() { cancel(); <-fileTransferWorkers; close(job.done) }()
			f, v, e := stableSourceFile(work, d, r, c, "")
			if e != nil {
				job.err = e
				return
			}
			defer os.Remove(f.Name())
			defer f.Close()
			job.version = v
			job.bytes, job.err = io.ReadAll(io.LimitReader(f, maxSourceFileBytes+1))
		}()
	}
	job := s.native
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-job.done:
	}
	s.native = nil
	if job.err != nil {
		return job.err
	}
	if expected != "" && job.version != expected {
		return invalid("cursor", "fileVersionChanged")
	}
	if s.file != nil && s.version == job.version {
		return nil
	}
	return s.installBytes(job.bytes, job.version, "")
}

func (s *fileSnapshot) installBytes(data []byte, version, etag string) error {
	f, e := os.CreateTemp("", "tier0-edge-file-*")
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		os.Remove(f.Name())
		return e
	}
	s.install(f, version, etag)
	return nil
}

func strongFileETag(tag string) bool {
	if len(tag) < 2 || len(tag) > 256 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for _, c := range []byte(tag[1 : len(tag)-1]) {
		if c <= 32 || c == 127 || c == '"' {
			return false
		}
	}
	return true
}
func (s *fileSnapshot) forgetPartial() { s.partial = bytes.Buffer{}; s.partialETag = ""; s.total = 0 }

// Only a strong validator permits byte-range resumption. Peers with no
// reliable version are read twice in full and never get partial bytes appended.
func (s *fileSnapshot) prepareHTTP(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) (bool, error) {
	if s.partialETag != "" && int64(s.partial.Len()) == s.total {
		return true, s.confirmHTTP(ctx, d, r, c, expected)
	}
	headers := http.Header{}
	offset := int64(s.partial.Len())
	if s.partialETag != "" {
		headers.Set("If-Match", s.partialETag)
		headers.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	} else if s.etag != "" {
		headers.Set("If-None-Match", s.etag)
	}
	resp, e := fileHTTPGet(ctx, d, r, c, headers)
	if e != nil {
		return true, e
	}
	defer resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if resp.StatusCode == http.StatusNotModified && s.etag != "" && s.partialETag == "" && s.version != "" {
		if etag != "" && etag != s.etag {
			return true, invalid("file", "invalidValidator")
		}
		if expected != "" && s.version != expected {
			return true, invalid("cursor", "fileVersionChanged")
		}
		return true, nil
	}
	if resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		s.forgetPartial()
		return true, invalid("file", "fileVersionChanged")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return true, fmt.Errorf("file HTTP status %d", resp.StatusCode)
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return true, invalid("file", "unsupportedEncoding")
	}
	if s.partialETag != "" && etag != s.partialETag {
		s.forgetPartial()
		return true, invalid("file", "fileVersionChanged")
	}
	if !strongFileETag(etag) {
		return false, nil
	}
	if etag == s.etag && s.version != "" {
		if expected != "" && s.version != expected {
			return true, invalid("cursor", "fileVersionChanged")
		}
		return true, nil
	}
	if resp.StatusCode == http.StatusPartialContent {
		// An open-ended range must reach the declared EOF for this same ETag.
		if s.partialETag == "" {
			return true, invalid("file", "invalidRange")
		}
		want := fmt.Sprintf("bytes %d-%d/%d", offset, s.total-1, s.total)
		if resp.Header.Get("Content-Range") != want || resp.ContentLength != s.total-offset {
			s.forgetPartial()
			return true, invalid("file", "invalidRange")
		}
	} else {
		s.forgetPartial()
		offset = 0
		if resp.ContentLength < 0 {
			return false, nil
		}
		s.partialETag = etag
		s.total = resp.ContentLength
	}
	if s.total > maxSourceFileBytes {
		s.forgetPartial()
		return true, invalid("file", "tooLarge")
	}
	if s.progress != nil {
		s.progress.received.Store(offset)
		s.progress.total.Store(s.total)
	}
	_, e = io.Copy(fileSnapshotProgressWriter{s}, io.LimitReader(resp.Body, s.total-offset+1))
	if int64(s.partial.Len()) > s.total {
		s.forgetPartial()
		return true, invalid("file", "sizeMismatch")
	}
	if e != nil {
		return true, e
	}
	if int64(s.partial.Len()) != s.total {
		return true, io.ErrUnexpectedEOF
	}
	return true, s.confirmHTTP(ctx, d, r, c, expected)
}

func (s *fileSnapshot) confirmHTTP(ctx context.Context, d SourceDefinition, r FileSelection, c Credential, expected string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}
	headers := http.Header{}
	headers.Set("If-None-Match", s.partialETag)
	resp, e := fileHTTPGet(ctx, d, r, c, headers)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != s.partialETag {
		s.forgetPartial()
		return invalid("file", "invalidValidator")
	}
	if resp.StatusCode != http.StatusNotModified && !(resp.StatusCode == http.StatusOK && resp.Header.Get("ETag") == s.partialETag) {
		s.forgetPartial()
		return invalid("file", "writeInProgress")
	}
	sum := sha256.Sum256(s.partial.Bytes())
	version := hex.EncodeToString(sum[:])
	if expected != "" && version != expected {
		s.forgetPartial()
		return invalid("cursor", "fileVersionChanged")
	}
	f, e := os.CreateTemp("", "tier0-edge-file-*")
	if e != nil {
		return e
	}
	if _, e = f.Write(s.partial.Bytes()); e != nil {
		f.Close()
		os.Remove(f.Name())
		return e
	}
	s.install(f, version, s.partialETag)
	s.forgetPartial()
	return nil
}

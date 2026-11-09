package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"backend/internal/streamexec"
)

func TestBoundedOutputExactLimitAndOverflow(t *testing.T) {
	canceled := 0
	w := &boundedOutput{limit: 8, cancel: func() { canceled++ }}
	for _, input := range []string{"123", "45678"} {
		if n, err := w.Write([]byte(input)); err != nil || n != len(input) {
			t.Fatalf("bounded write: n=%d err=%v", n, err)
		}
	}
	if string(w.data) != "12345678" || cap(w.data) != 8 || canceled != 0 {
		t.Fatal("exact-limit output must remain within fixed capacity without cancellation")
	}
	if n, err := w.Write([]byte("overflow")); n != 0 || !errors.Is(err, streamexec.ErrOutput) || canceled != 1 {
		t.Fatalf("overflow must cancel once: n=%d err=%v cancels=%d", n, err, canceled)
	}
	if n, err := w.Write([]byte("again")); n != 0 || !errors.Is(err, streamexec.ErrOutput) || canceled != 1 || len(w.data) != 8 {
		t.Fatal("overflow must never append or cancel twice")
	}
}

func TestBoundedOutputPartialWrite(t *testing.T) {
	canceled := false
	w := &boundedOutput{limit: 4, cancel: func() { canceled = true }}
	if n, err := w.Write([]byte("abcdef")); n != 4 || !errors.Is(err, streamexec.ErrOutput) || !canceled || string(w.data) != "abcd" || cap(w.data) != 4 {
		t.Fatalf("partial overflow: n=%d err=%v", n, err)
	}
}

// This is a real child process fixture, never an application execution fallback.
func TestBoundedCommandChild(t *testing.T) {
	if os.Getenv("TIER0_EXECUTOR_TEST_CHILD") != "1" {
		return
	}
	switch os.Getenv("TIER0_EXECUTOR_TEST_MODE") {
	case "exact":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", streamexec.MaxJSONBytes+16*1024))
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("ignored-private-diagnostic", 100000))
	case "overflow":
		chunk := bytes.Repeat([]byte("x"), 64*1024)
		for {
			_, _ = os.Stdout.Write(chunk)
		}
	case "wait":
		time.Sleep(time.Hour)
	case "fail":
		_, _ = fmt.Fprint(os.Stderr, "private child diagnostic must never appear in response")
		os.Exit(7)
	}
	os.Exit(0)
}

func childCommand(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBoundedCommandChild$")
	cmd.Env = append(os.Environ(), "TIER0_EXECUTOR_TEST_CHILD=1", "TIER0_EXECUTOR_TEST_MODE="+mode)
	return cmd
}

func TestBoundedChildOutput(t *testing.T) {
	for _, mode := range []string{"exact", "overflow", "wait", "fail"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "wait" {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			cmd := childCommand(ctx, mode)
			started := time.Now()
			data, err := boundedChildOutput(cmd, cancel)
			if cmd.ProcessState == nil {
				t.Fatal("child must have been waited and reaped")
			}
			switch mode {
			case "exact":
				if err != nil || len(data) != streamexec.MaxJSONBytes+16*1024 || ctx.Err() != nil {
					t.Fatalf("exact-limit child: bytes=%d err=%v", len(data), err)
				}
			case "overflow":
				if !errors.Is(err, streamexec.ErrOutput) || data != nil || ctx.Err() == nil || time.Since(started) > 3*time.Second {
					t.Fatalf("overflow must promptly kill and fail: err=%v duration=%v", err, time.Since(started))
				}
			case "wait":
				if err == nil || ctx.Err() == nil || time.Since(started) > 3*time.Second {
					t.Fatal("deadline must promptly terminate the child")
				}
			case "fail":
				if err == nil || strings.Contains(err.Error(), "private") || len(data) != 0 {
					t.Fatal("stderr must not be buffered or returned")
				}
			}
		})
	}
}

func TestExecutionEndpointsShareOneSlot(t *testing.T) {
	capacity := make(chan struct{}, 1)
	capacity <- struct{}{}
	for name, handler := range map[string]http.HandlerFunc{
		"execute":            executeHandler("not-started", capacity),
		"execute-controlled": controlledHandler("not-started", capacity),
	} {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodPost, "/"+name, strings.NewReader(`{}`)))
		if recorder.Code != http.StatusServiceUnavailable || len(capacity) != 1 {
			t.Fatalf("%s must reject while the shared slot is occupied", name)
		}
	}
	<-capacity
	for _, input := range []string{`invalid`, `{"timeoutMs":0}`, `{"timeoutMs":1,"unknown":true}`, `{"timeoutMs":1} {}`} {
		recorder := httptest.NewRecorder()
		executeHandler("not-started", capacity)(recorder, httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(input)))
		if recorder.Code != http.StatusBadRequest || len(capacity) != 0 {
			t.Fatal("validation failure must release the single slot")
		}
	}
	// A child startup failure also releases admission and exposes no host detail.
	recorder := httptest.NewRecorder()
	executeHandler("/nonexistent-tier0-test-executor", capacity)(recorder, httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"timeoutMs":1}`)))
	if recorder.Code != http.StatusOK || len(capacity) != 0 || !strings.Contains(recorder.Body.String(), streamexec.ErrExecution.Error()) || strings.Contains(recorder.Body.String(), "nonexistent") {
		t.Fatal("child startup failure must release the slot and remain sanitized")
	}
}

// 真实 HTTP 栈会在普通写响应前消费请求 Body，Recorder 无法覆盖此行为。
// 忙槽必须在双工请求还未 EOF 时及时拒绝，而不是等待调用方传输超时。
func TestBusyExecutionRejectsOpenRequestBody(t *testing.T) {
	testExecutionOpenBody(t, true)
}

func TestInvalidExecutionRejectsOpenRequestBody(t *testing.T) {
	testExecutionOpenBody(t, false)
}

func testExecutionOpenBody(t *testing.T, busy bool) {
	t.Helper()
	for _, endpoint := range []string{"/execute", "/execute-controlled", "/execute-sources"} {
		t.Run(strings.TrimPrefix(endpoint, "/"), func(t *testing.T) {
			capacity := make(chan struct{}, 1)
			expectedSlots, expectedStatus := 0, http.StatusBadRequest
			if busy {
				capacity <- struct{}{}
				expectedSlots, expectedStatus = 1, http.StatusServiceUnavailable
			}
			mux := http.NewServeMux()
			mux.HandleFunc("POST /execute", executeHandler("not-started", capacity))
			mux.HandleFunc("POST /execute-controlled", controlledHandler("not-started", capacity))
			registerSourceExecution(mux, "not-started", capacity)
			server := httptest.NewServer(mux)
			defer server.Close()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			go func() { _, _ = io.WriteString(writer, "{}\n") }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+endpoint, reader)
			if err != nil {
				t.Fatal(err)
			}
			type response struct {
				value *http.Response
				err   error
			}
			done := make(chan response, 1)
			started := time.Now()
			go func() { value, err := server.Client().Do(request); done <- response{value, err} }()
			var result response
			select {
			case result = <-done:
			case <-time.After(500 * time.Millisecond):
				t.Error("rejection waited for request EOF instead of promptly returning its status")
				_ = writer.Close()
				result = <-done
			}
			_ = writer.Close()
			if result.err != nil {
				t.Fatalf("rejection response failed: %v", result.err)
			}
			defer result.value.Body.Close()
			if result.value.StatusCode != expectedStatus || len(capacity) != expectedSlots {
				t.Fatal("rejection changed shared admission or returned the wrong status")
			}
			if !result.value.Close {
				t.Fatal("early rejection must not reuse the open request connection")
			}
			t.Logf("rejection status=%d headers received in %s", result.value.StatusCode, time.Since(started))
		})
	}
}

package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/infra/streamexecutor"
	"backend/internal/streamexec"
)

type executionTestLog func([]byte) (int, error)

func (write executionTestLog) Write(data []byte) (int, error) { return write(data) }

// 实际 HTTP handler、真实子进程及客户端，检查终帧后的请求/响应收尾。
func TestControlledHandlerRealClientCompletesWithoutPanic(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "executor")
	if output, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build executor: %v %s", err, output)
	}
	dir, err := os.MkdirTemp("", "t0-http-end-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	logs := make(chan string, 64)
	capacity := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /execute-controlled", controlledHandler(binary, capacity))
	registerSourceExecution(mux, binary, capacity)
	server := &http.Server{Handler: mux, ErrorLog: log.New(executionTestLog(func(data []byte) (int, error) {
		select {
		case logs <- string(data):
		default:
		}
		return len(data), nil
	}), "", 0)}
	t.Cleanup(func() {
		_ = server.Close()
		for {
			select {
			case entry := <-logs:
				if strings.Contains(entry, "panic serving") {
					t.Error("actual handler panicked during terminal-frame cleanup: " + entry)
				}
			default:
				return
			}
		}
	})
	go func() { _ = server.Serve(listener) }()
	client := streamexecutor.New(socket)
	for i := 0; i < 20; i++ {
		_, err := client.ExecuteProgram(context.Background(), streamexec.ProgramRequest{
			Code:      `module.exports=api=>api.stream.onRequest({id:"read"},ctx=>ctx.input);`,
			CheckOnly: true, TimeoutMs: 1000,
		}, nil)
		if err != nil {
			t.Fatalf("registration %d: %v", i, err)
		}
		_, err = client.TransformWithLegacySources(context.Background(), sourcestream.ScriptDefinition{
			Code: `return {data:input,drop:false};`, TimeoutMs: 1000,
		}, map[string]any{"value": i}, nil, []string{}, nil)
		if err != nil {
			t.Fatalf("legacy execution %d: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestControlledTimeout(t *testing.T) {
	for _, tc := range []struct {
		message streamexec.Exchange
		valid   bool
	}{
		{streamexec.Exchange{Kind: "execute", Request: &streamexec.Request{TimeoutMs: 1}}, true},
		{streamexec.Exchange{Kind: "program-execute", ProgramRequest: &streamexec.ProgramRequest{TimeoutMs: 1}}, true},
		{streamexec.Exchange{Kind: "program-execute", ProgramRequest: &streamexec.ProgramRequest{TimeoutMs: streamexec.MaxProgramTimeoutMs}}, true},
		{streamexec.Exchange{Kind: "program-execute", ProgramRequest: &streamexec.ProgramRequest{TimeoutMs: streamexec.MaxProgramTimeoutMs + 1}}, false},
		{streamexec.Exchange{Kind: "program-execute", ProgramRequest: &streamexec.ProgramRequest{TimeoutMs: streamexec.MaxTimeoutMs}}, true},
		{streamexec.Exchange{Kind: "execute", Request: &streamexec.Request{TimeoutMs: streamexec.MaxTimeoutMs}}, true},
		{streamexec.Exchange{Kind: "execute", Request: &streamexec.Request{TimeoutMs: streamexec.MaxTimeoutMs + 1}}, false},
		{streamexec.Exchange{Kind: "execute"}, false},
		{streamexec.Exchange{Kind: "program-execute"}, false},
		{streamexec.Exchange{Kind: "program-execute", ProgramRequest: &streamexec.ProgramRequest{TimeoutMs: 0}}, false},
		{streamexec.Exchange{Kind: "result"}, false},
	} {
		if _, valid := controlledTimeout(tc.message); valid != tc.valid {
			t.Fatalf("unexpected timeout validation for %s", tc.message.Kind)
		}
	}
}

func TestControlledReplyExactIDAndProtocol(t *testing.T) {
	call := streamexec.Exchange{Kind: "program-call", ProgramCall: &streamexec.ProgramCall{ID: 2}}
	valid := streamexec.Exchange{Kind: "program-reply", ProgramReply: &streamexec.SourceReply{ID: 2}}
	if !matchingControlledReply(call, valid) {
		t.Fatal("matching v2 reply rejected")
	}
	for _, reply := range []streamexec.Exchange{
		{Kind: "program-reply", ProgramReply: &streamexec.SourceReply{ID: 1}},
		{Kind: "source-reply", Reply: &streamexec.SourceReply{ID: 2}},
		{Kind: "program-reply"},
		{Kind: "program-reply", ProgramReply: &streamexec.SourceReply{ID: 2, Data: json.RawMessage(strings.Repeat("x", streamexec.MaxProgramDataBytes+1))}},
	} {
		if matchingControlledReply(call, reply) {
			t.Fatal("mismatched, missing or oversized reply accepted")
		}
	}
	legacy := streamexec.Exchange{Kind: "source-read", Call: &streamexec.SourceCall{ID: 1}}
	if !matchingControlledReply(legacy, streamexec.Exchange{Kind: "source-reply", Reply: &streamexec.SourceReply{ID: 1}}) || matchingControlledReply(legacy, valid) {
		t.Fatal("legacy replies must remain separate")
	}
}

func TestControlledProgramActualChild(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "executor")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual executor: %v %s", err, output)
	}
	request := streamexec.ProgramRequest{Code: `module.exports=function(api){
 const s=api.sources.use({id:"1",resources:["a","b"]});
 api.stream.onRequest({id:"read"},async function(){
  const [a,b]=await Promise.all([s.read("a"),s.read("b")]);return {a:a.value,b:b.value};
 });};`, EntryID: "read", Input: json.RawMessage(`{}`), Context: json.RawMessage(`{}`), TimeoutMs: 1000}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request.CheckOnly = true
	checked := runControlledExchange(ctx, binary, streamexec.Exchange{Kind: "program-execute", ProgramRequest: &request}, nil)
	if checked.ProgramResult == nil || checked.ProgramResult.Error != "" || len(checked.ProgramResult.Manifest.Entries) != 1 {
		t.Fatalf("check-only actual child failed: %+v", checked.ProgramResult)
	}
	request.CheckOnly = false
	request.ExpectedManifest = &checked.ProgramResult.Manifest
	seen := map[int]bool{}
	result := runControlledExchange(ctx, binary, streamexec.Exchange{Kind: "program-execute", ProgramRequest: &request}, func(call streamexec.Exchange) (streamexec.Exchange, error) {
		if call.Kind != "program-call" || !streamexec.AllowsProgramCall(*request.ExpectedManifest, *call.ProgramCall) || seen[call.ProgramCall.ID] {
			t.Fatal("actual child produced an invalid or duplicate call")
		}
		seen[call.ProgramCall.ID] = true
		return streamexec.Exchange{Kind: "program-reply", ProgramReply: &streamexec.SourceReply{ID: call.ProgramCall.ID, Data: json.RawMessage(`{"value":42}`)}}, nil
	})
	if result.ProgramResult == nil || result.ProgramResult.Error != "" || len(seen) != 2 || string(result.ProgramResult.Data) != `{"a":42,"b":42}` {
		t.Fatalf("async actual child failed: %+v", result.ProgramResult)
	}
	legacy := runControlledCommand(ctx, binary, streamexec.Request{Code: `return {data:input,drop:false};`, Input: json.RawMessage(`{"v":7}`), TimeoutMs: 1000}, nil)
	if legacy.Error != "" || string(legacy.Data) != `{"v":7}` {
		t.Fatalf("legacy child regressed: %+v", legacy)
	}
}

// Shell fixtures model malicious/broken pipe output, not a business fallback.
func TestControlledProgramWireBoundaries(t *testing.T) {
	call2 := `printf '%s\n' '{"kind":"program-call","programCall":{"id":2,"kind":"source.read"}}'; read reply; `
	call1 := `printf '%s\n' '{"kind":"program-call","programCall":{"id":1,"kind":"source.read"}}'; read reply; `
	final := `printf '%s\n' '{"kind":"program-result","programResult":{"outputs":[],"logs":[]}}'`
	for _, tc := range []struct {
		name, body, want string
		badReply         bool
	}{
		{"out-of-order", call2 + call1 + final, "", false},
		{"duplicate", call1 + call1 + final, streamexec.ErrSourceDenied.Error(), false},
		{"out-of-range", `printf '%s\n' '{"kind":"program-call","programCall":{"id":65,"kind":"source.read"}}'`, streamexec.ErrSourceDenied.Error(), false},
		{"mixed-protocol", `printf '%s\n' '{"kind":"result","result":{}}'`, streamexec.ErrSourceDenied.Error(), false},
		{"trailing-frame", final + "; " + final, streamexec.ErrOutput.Error(), false},
		{"wrong-reply-id", call1 + final, streamexec.ErrSourceCall.Error(), true},
		{"cancel", "read next", streamexec.ErrTimeout.Error(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "pipe-fixture")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nread initial\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			timeout := time.Second
			if tc.name == "cancel" {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			request := streamexec.ProgramRequest{TimeoutMs: 100}
			seen := []int{}
			result := runControlledExchange(ctx, binary, streamexec.Exchange{Kind: "program-execute", ProgramRequest: &request}, func(call streamexec.Exchange) (streamexec.Exchange, error) {
				id := call.ProgramCall.ID
				seen = append(seen, id)
				if tc.badReply {
					id++
				}
				return streamexec.Exchange{Kind: "program-reply", ProgramReply: &streamexec.SourceReply{ID: id, Data: json.RawMessage(`{}`)}}, nil
			})
			if result.ProgramResult == nil || result.ProgramResult.Error != tc.want {
				t.Fatalf("wire boundary %s: %+v, want %s", tc.name, result.ProgramResult, tc.want)
			}
			if tc.name == "out-of-order" && (len(seen) != 2 || seen[0] != 2 || seen[1] != 1) {
				t.Fatal("legitimate concurrent IDs must not require arrival order")
			}
		})
	}
}

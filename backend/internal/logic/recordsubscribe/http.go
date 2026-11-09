// Package recordsubscribe exposes the durable record-change reader as SSE.
package recordsubscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"backend/internal/domain/unsv2"
	respx "backend/internal/httpx"
)

const mediaType = "text/event-stream"
const frameWriteTimeout = 30 * time.Second

var connections = make(chan struct{}, 500)

// Serve uses the same authenticated context as ordinary UNS reads. It never
// accepts identity, data scope, cursor authority, or Fleet delegation from JSON.
func Serve(w http.ResponseWriter, r *http.Request, service *unsv2.Service) {
	respx.PrepareUnsV2(r)
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	reject := func(code, message string) {
		respx.WriteUnsV2Error(w, r.Context(), &unsv2.Fault{Code: code, Message: message, Reason: message})
	}
	if service == nil {
		reject("TEMPORARILY_UNAVAILABLE", "Record subscriptions are unavailable")
		return
	}
	if r.Method != http.MethodGet || r.Header.Get("Accept") != mediaType {
		reject("INVALID_REQUEST", "GET with Accept: text/event-stream required")
		return
	}
	if len(r.Header.Values("X-Fleet-Node-Id")) > 0 {
		reject("UNSUPPORTED_OPERATION", "Record subscriptions execute on the current Backend")
		return
	}
	if len(r.URL.RawQuery) > 32768 || r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		reject("INVALID_REQUEST", "Invalid subscription request")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		reject("INVALID_REQUEST", "Invalid subscription query")
		return
	}
	for key := range query {
		if key != "collectionIds" && key != "recordIds" {
			reject("INVALID_REQUEST", "Only collectionIds and recordIds are accepted query parameters")
			return
		}
	}
	if len(r.Header.Values("Last-Event-ID")) > 1 {
		reject("INVALID_REQUEST", "One Last-Event-ID header is required")
		return
	}
	select {
	case connections <- struct{}{}:
		defer func() { <-connections }()
	default:
		w.Header().Set("Retry-After", "5")
		reject("OVERLOADED", "Too many record subscriptions")
		return
	}
	request := unsv2.RecordSubscribeRequest{CollectionIDs: query["collectionIds"], RecordIDs: query["recordIds"], Cursor: r.Header.Get("Last-Event-ID"), Limit: unsv2.RecordSubscribeMaxEvents}
	response, err := service.RecordSubscribe(r.Context(), request)
	if err != nil {
		respx.WriteUnsV2Error(w, r.Context(), err)
		return
	}
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(time.Time{}) != nil || controller.SetWriteDeadline(time.Now().Add(frameWriteTimeout)) != nil {
		reject("TEMPORARILY_UNAVAILABLE", "Streaming is unavailable on this connection")
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	frame := func(event, id string, data any) bool {
		raw, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if err = controller.SetWriteDeadline(time.Now().Add(frameWriteTimeout)); err != nil {
			return false
		}
		if id != "" {
			_, err = fmt.Fprintf(w, "event: %s\nid: %s\ndata: %s\n\n", event, id, raw)
		} else {
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		}
		return err == nil
	}
	first := true
	for {
		if response.Reset != nil {
			if frame("reset", "", response.Reset) {
				_ = controller.Flush()
			}
			return
		}
		for _, event := range response.Events {
			if !frame("change", event.EventID, event) {
				return
			}
		}
		if response.Cursor == nil {
			return
		}
		if first || len(response.Events) == 0 {
			if !frame("heartbeat", *response.Cursor, map[string]int64{"at": time.Now().UnixMilli()}) {
				return
			}
		}
		if controller.Flush() != nil {
			return
		}
		first = false
		request.Cursor = *response.Cursor
		request.WaitMS = unsv2.RecordSubscribeMaxWaitMS
		// The reader reserves three seconds for its response. Stop before that
		// margin, otherwise an expired polling budget would emit heartbeats in a loop.
		deadline, _ := r.Context().Deadline()
		if r.Context().Err() != nil || time.Until(deadline) <= 3*time.Second {
			return
		}
		response, err = service.RecordSubscribe(r.Context(), request)
		if err != nil {
			return
		} // Preserve Last-Event-ID on transient failures; never fabricate a reset.
	}
}

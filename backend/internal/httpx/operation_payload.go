package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// ReadOperationPayload reads business JSON only. Path identity and control
// headers are parsed separately and cannot be overwritten by body fields.
func ReadOperationPayload(r *http.Request) (any, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Body == nil {
		return nil, errors.New("application/json body required")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, sourceStreamBodyLimit+1))
	if err != nil || len(raw) > sourceStreamBodyLimit {
		return nil, errors.New("invalid or oversized operation body")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("one JSON value required")
	}
	return value, nil
}

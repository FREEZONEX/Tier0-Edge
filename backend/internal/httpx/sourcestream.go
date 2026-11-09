package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

const sourceStreamBodyLimit = 1 << 20

// ParseSourceStreamRequest supports opaque protocol documents without changing
// the API contract. Only JSON-tagged fields may come from the body: identifiers
// from the route must never be overwritten by a client-supplied JSON field.
func ParseSourceStreamRequest(r *http.Request, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return errors.New("request target must be a struct pointer")
	}
	v = v.Elem()
	t := v.Type()
	hasJSON := false
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("json"); tag != "" && tag != "-" {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		return gozerohttpx.Parse(r, target)
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("application/json required")
	}
	if r.Body == nil {
		return errors.New("JSON object required")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, sourceStreamBodyLimit+1))
	if err != nil {
		return err
	}
	if len(body) > sourceStreamBodyLimit {
		return errors.New("request body too large")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("JSON object required")
	}
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("json"), ",")
		if tag[0] == "" || tag[0] == "-" {
			continue
		}
		raw, exists := fields[tag[0]]
		optional := false
		for _, option := range tag[1:] {
			optional = optional || option == "optional"
		}
		if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if !optional {
				return fmt.Errorf("required field %s", tag[0])
			}
			continue
		}
		if err = json.Unmarshal(raw, v.Field(i).Addr().Interface()); err != nil {
			return fmt.Errorf("invalid field %s: %w", tag[0], err)
		}
	}
	if err = gozerohttpx.ParsePath(r, target); err != nil {
		return err
	}
	if err = gozerohttpx.ParseForm(r, target); err != nil {
		return err
	}
	return gozerohttpx.ParseHeaders(r, target)
}

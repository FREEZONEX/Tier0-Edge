package sourcestream

import (
	"backend/internal/repo"
	"backend/internal/streamexec"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"strconv"
)

// Shared transforms reuse exactly the managed executor and connection drivers
// used by the Source/Stream lifecycle APIs. Authorization/configuration fencing
// and checkpoints belong to the caller, not this execution adapter.
func (s *Service) SharedTransform(ctx context.Context, code string, timeout int, input any, params map[string]any) (ScriptResult, error) {
	if s.executor == nil {
		return ScriptResult{}, ErrUnavailable
	}
	p, err := json.Marshal(params)
	if err != nil {
		return ScriptResult{}, err
	}
	body := "const params=" + string(p) + ";\nconst value=(function(input,params){\n" + code + "\n})(input,params);\nreturn {data:value,drop:false};"
	return s.executor.Transform(ctx, ScriptDefinition{Code: body, TimeoutMs: timeout}, input, map[string]any{"sharedTransform": true})
}

func sharedDriverID(id string) int64 {
	if n, err := strconv.ParseInt(id, 10, 64); err == nil && n > 0 {
		return n
	}
	sum := sha256.Sum256([]byte("shared-source:" + id))
	return int64(binary.BigEndian.Uint64(sum[:8])&0x3fffffffffffffff) + 1
}

func (s *Service) SharedHTTP(ctx context.Context, id string, revision int64, def SourceDefinition, path string, query map[string]string) (any, error) {
	if def.Protocol != HTTP || def.SourceType != "" {
		options, _ := json.Marshal(HTTPRequestOptions{Method: "GET", Path: path, Query: query})
		raw, err := s.HTTPProgramCall(ctx, repo.Source{ID: sharedDriverID(id), ConfigRevision: revision}, def, streamexec.ProgramCall{Kind: "source.request", Options: options})
		if err != nil {
			return nil, err
		}
		var response HTTPResponse
		if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 {
			return nil, ErrHTTPRequest
		}
		return response.Data, nil
	}
	if def.CredentialRef != "" {
		if err := s.ValidateSharedCredentialReference(ctx, id, def); err != nil {
			return nil, err
		}
	}
	credential, err := s.sharedCredential(ctx, id, def.CredentialRef)
	if err != nil {
		return nil, err
	}
	options, _ := json.Marshal(HTTPRequestOptions{Method: "GET", Path: path, Query: query})
	raw, err := s.httpProgramCall(ctx, repo.Source{ID: sharedDriverID(id), ConfigRevision: revision}, def, streamexec.ProgramCall{Kind: "source.request", Options: options}, &credential)
	if err != nil {
		return nil, err
	}
	var response HTTPResponse
	if json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 {
		return nil, ErrHTTPRequest
	}
	return response.Data, nil
}

func (s *Service) SharedWatch(ctx context.Context, id string, def SourceDefinition, collection *CollectionDefinition, refs []string, emit func(SourceInput) error) error {
	runtime, ok := s.runtime.(interface {
		Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
	})
	if !ok {
		return ErrUnavailable
	}
	if err := s.ValidateSharedCredentialReference(ctx, id, def); err != nil {
		return err
	}
	credential, err := s.sharedCredential(ctx, id, def.CredentialRef)
	if err != nil {
		return err
	}
	effective, err := (StreamDefinition{Model: Publish, Collection: collection, NativeResourceRefs: refs}).EffectiveSource(def)
	if err != nil {
		return err
	}
	return runtime.Watch(ctx, sharedDriverID(id), effective, credential, emit)
}

// Native preview is an explicit connection test, not invented sample data. The
// existing runtime owns protocol validation, authentication and cancellation.
func (s *Service) SharedProbe(ctx context.Context, id string, def SourceDefinition) error {
	if s.runtime == nil {
		return ErrUnavailable
	}
	if err := s.ValidateSharedCredentialReference(ctx, id, def); err != nil {
		return err
	}
	credential, err := s.sharedCredential(ctx, id, def.CredentialRef)
	if err != nil {
		return err
	}
	return s.runtime.Probe(ctx, sharedDriverID(id), def.Connection(), credential)
}

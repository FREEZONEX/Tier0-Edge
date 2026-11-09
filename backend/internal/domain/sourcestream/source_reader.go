package sourcestream

import (
	"context"
	"encoding/json"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

// SourceReader 只用于已授权的交互式读取；后台执行需使用生效版本的依赖快照，
// 不能伪造用户 Subject 或将调用方提供的 grants 当作部署授权。
func (s *Service) SourceReader(ctx context.Context, grants []streamexec.SourceGrant) (streamexec.SourceHost, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read") || !contextx.HasResource(subject, "uns.stream.manage") {
		return nil, ErrForbidden
	}
	return s.sourceReader(grants)
}

// Private broker: callers must either authorize an interactive request above,
// or validate the immutable deployed version before providing its grants.
func (s *Service) sourceReader(grants []streamexec.SourceGrant) (streamexec.SourceHost, error) {
	if !streamexec.ValidSourceGrants(grants) {
		return nil, streamexec.ErrSourceDenied
	}
	// 清单由本次执行持有，外部切片后续修改不能扩大权限。
	encoded, _ := json.Marshal(grants)
	var allowed []streamexec.SourceGrant
	_ = json.Unmarshal(encoded, &allowed)
	return func(callCtx context.Context, call streamexec.SourceCall) (json.RawMessage, error) {
		if s.runtime == nil {
			return nil, ErrUnavailable
		}
		if !streamexec.AllowsSourceRead(allowed, call) {
			return nil, streamexec.ErrSourceDenied
		}
		// 只读操作由平台选择。脚本不能通过 input.operation 将读取变成写入。
		var options map[string]json.RawMessage
		if json.Unmarshal(call.Input, &options) != nil || options == nil || len(options) != 0 {
			return nil, streamexec.ErrSourceDenied
		}
		var grant streamexec.SourceGrant
		for _, item := range allowed {
			if item.Alias == call.Alias {
				grant = item
				break
			}
		}
		r := repo.NewSourceStreamRepo(callCtx)
		source, err := r.Source(callCtx, grant.SourceID)
		if err != nil {
			return nil, streamexec.ErrSourceDenied
		}
		if source.ConfigRevision != grant.ConfigRevision {
			return nil, repo.ErrConflict
		}
		// MQTT 没有通用的只读 operation；应使用平台维护的消息订阅入口。
		if source.Protocol != string(OPCUA) && source.Protocol != string(ModbusTCP) {
			return nil, streamexec.ErrSourceDenied
		}
		var resources []NativeResource
		if json.Unmarshal([]byte(source.Resources), &resources) != nil {
			return nil, repo.ErrInvalidArgument
		}
		readable := false
		for _, resource := range resources {
			if resource.ID == call.ResourceRef {
				for _, model := range resource.Models {
					readable = readable || model == Publish
				}
			}
		}
		if !readable {
			return nil, streamexec.ErrSourceDenied
		}
		def, credential, _, err := s.connection(callCtx, source.ID, nil)
		if err != nil {
			return nil, ErrCredential
		}
		// 连接配置和凭证读取后再次检查配置版本，避免在竞态中使用更新后的配置。
		current, err := r.Source(callCtx, source.ID)
		if err != nil || current.ConfigRevision != grant.ConfigRevision {
			return nil, repo.ErrConflict
		}
		value, err := s.runtime.Invoke(callCtx, source.ID, def, credential, call.ResourceRef, map[string]any{"operation": "read"})
		if err != nil {
			return nil, streamexec.ErrSourceCall
		}
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > streamexec.MaxJSONBytes {
			return nil, streamexec.ErrSourceCall
		}
		return raw, nil
	}, nil
}

package sourcestream

import (
	"context"
	"errors"
	"time"
)

var sampleComplete = errors.New("sample complete")

// 临时采集只返回输入，不进入 NATS、不写 UNS、不更改 Source 健康状态。
func (s *Service) sample(ctx context.Context, def StreamDefinition) (SourceInput, error) {
	return s.sampleWithin(ctx, def, 8*time.Second)
}

func (s *Service) sampleWithin(ctx context.Context, def StreamDefinition, timeout time.Duration) (SourceInput, error) {
	var empty SourceInput
	if def.Model != Publish {
		return empty, invalid("phase", "unsupported")
	}
	runtime, ok := s.runtime.(interface {
		Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
	})
	if !ok {
		return empty, ErrUnavailable
	}
	source, credential, _, err := s.connection(ctx, def.SourceID, nil)
	if err != nil {
		return empty, err
	}
	runtimeSource, err := def.EffectiveSource(source)
	if err != nil {
		return empty, err
	}
	if runtimeSource.MQTT != nil {
		mqtt := *runtimeSource.MQTT
		mqtt.Ephemeral = true
		runtimeSource.MQTT = &mqtt
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	samples := make(chan SourceInput, 1)
	err = runtime.Watch(ctx, def.SourceID, runtimeSource, credential, func(input SourceInput) error {
		if !def.MatchesResource(input.ResourceRef) {
			return nil
		}
		select {
		case samples <- input:
		default:
		}
		return sampleComplete
	})
	select {
	case input := <-samples:
		return input, nil
	default:
	}
	if err == nil {
		err = ErrUnavailable
	}
	return empty, err
}

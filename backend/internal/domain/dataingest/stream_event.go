package dataingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type StreamEvent struct {
	Origin          string    `json:"origin,omitempty"`
	MessageID       string    `json:"messageId"`
	SourceID        int64     `json:"sourceId"`
	StreamID        int64     `json:"streamId"`
	VersionID       int64     `json:"versionId"`
	TopicID         int64     `json:"topicId"`
	MetadataTopicID string    `json:"metadataTopicId,omitempty"`
	CollectionID    string    `json:"collectionId,omitempty"`
	RecordID        string    `json:"recordId,omitempty"`
	ReceivedAt      time.Time `json:"receivedAt"`
	Data            any       `json:"data"`
	Drop            bool      `json:"drop,omitempty"`
	Lineage         []int64   `json:"lineage,omitempty"`
}

// Bound chained UNS-triggered processing and prevent event feedback loops.
const MaxStreamLineage = 32

// EnableResourceEvents is wired only for the Source/Stream deployment mode.
// Legacy deployments retain their existing write and notification behavior.
func (s *Service) EnableResourceEvents(ctx context.Context) error {
	if err := s.EnsureStreamStorage(ctx); err != nil {
		return err
	}
	s.resourceEvents.Store(true)
	return nil
}

func (s *Service) commitNativeWrite(ctx context.Context, node repo.UnsNode, data any, records []Record) error {
	event := StreamEvent{
		Origin: "native", MessageID: "native:" + uuid.NewString(),
		TopicID: node.ID, ReceivedAt: time.Now().UTC(), Data: data,
	}
	raw, err := json.Marshal(event)
	if err != nil || len(raw) > 1024*1024 {
		return errors.New("SourceStream.invalidOutput")
	}
	item := StreamCommit{Native: true, ReceiptID: event.MessageID, TopicID: node.ID,
		Subject: fmt.Sprintf("tier0.events.topic.%d", node.ID), Event: raw}
	if definitionFromNode(node).SaveToDB {
		item.Records = records
	}
	created, err := s.writer.CommitStreamTarget(ctx, item)
	if err == nil && created {
		s.publishCommittedResourceEvent(ctx, item)
	}
	return err
}

func (s *Service) EnsureStreamStorage(ctx context.Context) error {
	if s == nil || s.writer == nil {
		return errors.New("SourceStream.sinkUnavailable")
	}
	return s.writer.EnsureStreamSchema(ctx)
}

// CommitStreamEvent respects the Topic's history policy. Forward-only events
// persist a processing receipt but no history row. Notifications are best effort.
func (s *Service) CommitStreamEvent(ctx context.Context, node repo.UnsNode, event StreamEvent) (bool, error) {
	if s == nil || s.writer == nil {
		return false, errors.New("SourceStream.sinkUnavailable")
	}
	item, records, err := prepareStreamEvent(ctx, node, event)
	if err != nil {
		return false, err
	}
	created, err := s.writer.CommitStreamTarget(ctx, item)
	if err != nil || !created {
		return created, err
	}
	s.notifyCommittedStreamEvent(ctx, node, records)
	s.publishCommittedResourceEvent(ctx, item)
	return true, nil
}

func prepareStreamEvent(ctx context.Context, node repo.UnsNode, event StreamEvent) (StreamCommit, []Record, error) {
	if node.Type != 2 || node.ID != event.TopicID || event.ReceivedAt.IsZero() {
		return StreamCommit{}, nil, errors.New("SourceStream.invalidTarget")
	}
	if len(event.Lineage) > MaxStreamLineage {
		return StreamCommit{}, nil, errors.New("SourceStream.invalidTarget")
	}
	def := definitionFromNode(node)
	var records []Record
	var raw []byte
	var err error
	if !event.Drop {
		raw, err = json.Marshal(event.Data)
		if err != nil || len(raw) > 1024*1024 || string(raw) == "null" {
			return StreamCommit{}, nil, errors.New("SourceStream.invalidOutput")
		}
		records, err = NormalizePayload(ctx, def, raw)
		if err != nil || len(records) == 0 {
			return StreamCommit{}, nil, errors.New("SourceStream.schemaMismatch")
		}
		for i := range records {
			// Stable receive time survives redelivery. Array elements must not
			// collapse onto the metric table's (id,timestamp) primary key.
			records[i].Time = event.ReceivedAt.UTC().Truncate(time.Millisecond).Add(time.Duration(i) * time.Millisecond)
			records[i].IngestedAt = event.ReceivedAt.UTC()
			for _, field := range dataFields(def.Fields, def.TopicType) {
				value, found := records[i].Values[field.Name]
				if !found {
					return StreamCommit{}, nil, fmt.Errorf("SourceStream.schemaMismatch: %s", field.Name)
				}
				if _, err := typedValueByType(field.Type, value); err != nil {
					return StreamCommit{}, nil, fmt.Errorf("SourceStream.schemaMismatch: %s", field.Name)
				}
			}
		}
	}
	item := StreamCommit{ReceiptID: event.MessageID, StreamID: event.StreamID, VersionID: event.VersionID, TopicID: event.TopicID}
	if !event.Drop {
		item.Subject = fmt.Sprintf("tier0.events.topic.%d", node.ID)
		item.Event, err = json.Marshal(event)
		if err != nil || len(item.Event) > 1024*1024 {
			return StreamCommit{}, nil, errors.New("SourceStream.invalidOutput")
		}
		if def.SaveToDB {
			item.Records = records
		}
	}
	return item, records, nil
}

func (s *Service) notifyCommittedStreamEvent(ctx context.Context, node repo.UnsNode, records []Record) {
	def := definitionFromNode(node)
	// Cache/websocket notifications happen only after the sink committed.
	if len(records) > 0 {
		if err := s.writeLatestCache(ctx, records); err != nil {
			logx.WithContext(ctx).Errorf("stream latest cache update failed topic=%d", node.ID)
		}
		publishPayloadRecords(records)
		if s.qualityObserver != nil {
			observations := qualityObservations(records)
			s.qualityObserver.Observe(ctx, observations)
			if def.SaveToDB {
				s.qualityObserver.RecordPersisted(ctx, observations)
			}
		}
	}
}

func (s *Service) SetResourceEventPublisher(publish repo.ResourceEventPublisher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resourceEventPublisher = publish
}

func (s *Service) publishCommittedResourceEvent(ctx context.Context, item StreamCommit) {
	if item.Subject == "" {
		return
	}
	s.mu.Lock()
	publish := s.resourceEventPublisher
	s.mu.Unlock()
	if publish == nil || publish(ctx, item.Subject, item.ReceiptID, item.Event) != nil {
		logx.WithContext(ctx).Errorf("UNS Topic notification failed after commit: event=%s; no database retry", item.ReceiptID)
	}
}

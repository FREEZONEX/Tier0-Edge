package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// MetadataResourceEvent is a best-effort notification of committed UNS data.
type MetadataResourceEvent struct {
	MessageID    string
	CollectionID string
	RecordID     string
	SourceID     int64
	StreamID     int64
	VersionID    int64
	EntryID      string
	ReceivedAt   int64
	Data         any
	Change       string
	Lineage      []int64
}

// MetadataEventSubject hashes typed identities so display names and dots in IDs
// cannot expand the NATS routing scope. It fits the existing EVENTS filter.
func MetadataEventSubject(topicID, collectionID string) string {
	identity := "collection:" + collectionID
	if topicID != "" {
		identity = "topic:" + topicID
	}
	hash := sha256.Sum256([]byte(identity))
	return "tier0.events.topic.meta-" + hex.EncodeToString(hash[:])
}

// EnqueueMetadataResourceEvents collects bounded transaction-local notifications.
// It does not write an Outbox or schedule a persistent/background retry.
func EnqueueMetadataResourceEvents(ctx context.Context, tx *gorm.DB, event MetadataResourceEvent) error {
	if tx == nil || event.MessageID == "" || len(event.MessageID) > 128 || event.CollectionID == "" || len(event.CollectionID) > 128 || event.RecordID == "" || len(event.RecordID) > 128 || event.ReceivedAt <= 0 || len(event.Lineage) > 32 {
		return ErrInvalidArgument
	}
	var topics []UnsMeta
	if err := tx.WithContext(ctx).Where("kind='topic' AND deleted_at=0 AND data->>'collectionId'=? AND COALESCE(data->>'viewMode','records') IN ('records','')", event.CollectionID).Order("id").Find(&topics).Error; err != nil {
		return err
	}
	// Bound fan-out even if a malformed registry bypassed the normal UI limits.
	if len(topics) > 1000 {
		return ErrInvalidArgument
	}
	ids := []string{""}
	for _, topic := range topics {
		ids = append(ids, topic.ID)
	}
	for _, topicID := range ids {
		identity, err := json.Marshal([]string{event.MessageID, event.CollectionID, topicID})
		if err != nil {
			return err
		}
		idHash := sha256.Sum256(identity)
		id := "metadata:" + hex.EncodeToString(idHash[:])
		wire := map[string]any{"origin": "metadata", "messageId": id, "sourceId": event.SourceID, "streamId": event.StreamID, "versionId": event.VersionID,
			"topicId": int64(0), "metadataTopicId": topicID, "collectionId": event.CollectionID, "recordId": event.RecordID, "entryId": event.EntryID,
			"receivedAt": time.UnixMilli(event.ReceivedAt).UTC(), "data": event.Data, "change": event.Change, "lineage": event.Lineage}
		raw, err := json.Marshal(wire)
		if err != nil || len(raw) > 1024*1024 {
			return ErrInvalidArgument
		}
		if err := stageResourceNotification(tx, MetadataEventSubject(topicID, event.CollectionID), id, raw); err != nil {
			return err
		}
	}
	return nil
}

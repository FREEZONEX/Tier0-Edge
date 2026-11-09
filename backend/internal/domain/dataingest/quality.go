package dataingest

import (
	"context"
	"time"
)

// QualityObservation is a bounded ingest-stage view. Fields and RawValues are
// borrowed for the Observe call; an observer must clone them before retaining
// the observation for asynchronous evaluation.
type QualityObservation struct {
	UnsID         int64
	SourceTime    time.Time
	SourceTimeSet bool
	IngestedAt    time.Time
	CanonicalHash uint64
	QualityCode   int64
	Replayed      bool
	Fields        []Field
	RawValues     map[string]string
}

// QualityObserver keeps the data-quality module optional and avoids changing
// ingest behavior when DATA_QUALITY_ENABLED is false.
type QualityObserver interface {
	Observe(context.Context, []QualityObservation)
	RecordDrop(context.Context, string, string, []QualityObservation)
	RecordPersisted(context.Context, []QualityObservation)
}

// EffectiveQualityProvider overlays asynchronous platform detections on the
// latest source quality. It is intentionally read-only: historical sink rows
// keep the quality known when the sample was ingested.
type EffectiveQualityProvider interface {
	EffectiveQuality(unsID int64, sourceQuality int64) int64
}

const (
	ConnectorSourceMQTT = "mqtt"
	ConnectorSourceFlow = "flow"

	ConnectorStateConnected = "connected"
	ConnectorStateLost      = "lost"
	ConnectorStateDeployed  = "deployed"
	ConnectorStateDisabled  = "disabled"
	ConnectorStateError     = "error"
)

// ConnectorHealthSignal contains only bounded operational metadata. It never
// carries raw connector errors, credentials, topics, or payload values.
type ConnectorHealthSignal struct {
	Source     string
	State      string
	ObservedAt time.Time
	ErrorCode  string
	FlowID     int64
	UnsIDs     []int64
}

// ConnectorHealthObserver is separate from QualityObserver so existing ingest
// observers do not need to implement connector health. Implementations must
// enqueue without blocking the caller.
type ConnectorHealthObserver interface {
	RecordConnectorHealth(context.Context, ConnectorHealthSignal)
}

func qualityObservations(records []Record) []QualityObservation {
	if len(records) == 0 {
		return nil
	}
	out := make([]QualityObservation, 0, len(records))
	for _, record := range records {
		ingestedAt := record.IngestedAt
		if ingestedAt.IsZero() {
			ingestedAt = record.Time
		}
		qualityCode := record.Quality
		switch normalizeQuality(record.SourceQuality) {
		case qualityBad:
			qualityCode = 3
		case qualityUncertain:
			qualityCode = 2
		}
		out = append(out, QualityObservation{
			UnsID:         record.UnsID,
			SourceTime:    record.SourceTime,
			SourceTimeSet: record.SourceTimeSet,
			IngestedAt:    ingestedAt,
			QualityCode:   qualityCode,
			Replayed:      record.Replayed,
			Fields:        record.Fields,
			RawValues:     record.RawValues,
		})
	}
	return out
}

package dataingest

import (
	"context"
	"errors"

	"backend/internal/repo"
)

type ProgramStreamEvent struct {
	Node  repo.UnsNode
	Event StreamEvent
}

// Cursor-bearing pages form one atomic unit. Ordinary non-cursor outputs keep
// their existing per-Topic partial recovery behavior.
func (s *Service) CommitProgramCheckpoint(ctx context.Context, events []ProgramStreamEvent, change repo.ProgramCheckpointCommit) (bool, error) {
	if s == nil || s.writer == nil {
		return false, errors.New("SourceStream.sinkUnavailable")
	}
	if err := s.writer.assertCheckpointDatabase(ctx); err != nil {
		return false, err
	}
	items := make([]StreamCommit, len(events))
	prepared := make([][]Record, len(events))
	for i, event := range events {
		var err error
		items[i], prepared[i], err = prepareStreamEvent(ctx, event.Node, event.Event)
		if err != nil {
			return false, err
		}
	}
	created, err := s.writer.commitCheckpointTargets(ctx, items, change)
	if err != nil || !created {
		return created, err
	}
	for i, event := range events {
		s.notifyCommittedStreamEvent(ctx, event.Node, prepared[i])
		s.publishCommittedResourceEvent(ctx, items[i])
	}
	return true, nil
}

// UNS metadata and sink storage can be configured separately. Cursor-bearing
// pages require one database; never create a second cursor in an unrelated sink.
func (w *SinkWriter) assertCheckpointDatabase(ctx context.Context) error {
	if w == nil || w.pool == nil || repo.GetUnsConn(ctx) == nil {
		return errors.New("SourceStream.sinkUnavailable")
	}
	type identity struct {
		Name string
		Host string
		Port int
	}
	const query = `SELECT current_database() AS name,COALESCE(inet_server_addr()::text,'unix') AS host,COALESCE(inet_server_port(),0) AS port`
	var canonical, sink identity
	if err := repo.GetUnsConn(ctx).Raw(query).Scan(&canonical).Error; err != nil {
		return err
	}
	if err := w.pool.QueryRow(ctx, query).Scan(&sink.Name, &sink.Host, &sink.Port); err != nil {
		return err
	}
	if canonical != sink {
		return errors.New("SourceStream.checkpointDatabaseMismatch")
	}
	return nil
}

func (w *SinkWriter) commitCheckpointTargets(ctx context.Context, items []StreamCommit, change repo.ProgramCheckpointCommit) (bool, error) {
	if w == nil || w.pool == nil {
		return false, errors.New("SourceStream.sinkUnavailable")
	}
	if len(items) > 1000 {
		return false, repo.ErrInvalidArgument
	}
	allRecords := []Record{}
	for _, item := range items {
		if item.Native || item.ReceiptID == "" || item.StreamID != change.StreamID || item.VersionID <= 0 || item.TopicID <= 0 {
			return false, repo.ErrInvalidArgument
		}
		allRecords = append(allRecords, item.Records...)
	}
	mappings, err := w.ensurePhysicalForRecords(ctx, allRecords)
	if err != nil {
		return false, err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	created, finish, err := repo.LockProgramCheckpoint(ctx, tx, change)
	if err != nil || !created {
		return created, err
	}
	for _, item := range items {
		tag, err := tx.Exec(ctx, `INSERT INTO public.stream_receipt(receipt_id,stream_id,version_id,topic_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, item.ReceiptID, item.StreamID, item.VersionID, item.TopicID)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 0 {
			return false, repo.ErrConflict
		}
		for _, record := range item.Records {
			if err := insertStreamRecord(ctx, tx, record, mappings[record.UnsID]); err != nil {
				return false, err
			}
		}
	}
	if err := finish(); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

package repo

import "context"

// CapabilitySchemas uses constant, zero-row reads. It neither migrates nor scans
// assets, and verifies columns used by the current single Source/Stream model.
func (s *Store) CapabilitySchemas(ctx context.Context) map[string]bool {
	result := map[string]bool{}
	if s.UnsDB() == nil {
		return result
	}
	for code, query := range map[string]string{
		"source": "SELECT id,document,revision,config_revision,desired_state,health FROM public.source_projection LIMIT 0",
		"stream": "SELECT id,document,draft_manifest,active_version,pending_version,status FROM public.program_stream_projection LIMIT 0",
	} {
		rows, err := s.UnsDB().WithContext(ctx).Raw(query).Rows()
		result[code] = err == nil
		if rows != nil {
			_ = rows.Close()
		}
	}
	rows, err := s.UnsDB().WithContext(ctx).Raw("SELECT id,kind,data,revision FROM public.uns_meta LIMIT 0").Rows()
	result["namespace.v2"] = err == nil
	if rows != nil {
		_ = rows.Close()
	}
	return result
}

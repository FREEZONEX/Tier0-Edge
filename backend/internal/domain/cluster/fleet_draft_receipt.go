package cluster

import "backend/internal/repo"

type FleetRevisionConflict struct{ CurrentRevision int64 }

func (e *FleetRevisionConflict) Error() string { return "resource revision conflict" }
func (e *FleetRevisionConflict) Unwrap() error { return repo.ErrConflict }

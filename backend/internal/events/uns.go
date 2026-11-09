package events

import "backend/internal/repo"

type UnsNodeSaving struct {
	Action string
	Node   repo.UnsNode
	UserID int64
}

type UnsNodeDeleting struct {
	NodeID int64
	UserID int64
}

type UnsNodeRestoring struct {
	NodeID  int64
	UserID  int64
	Confirm bool
}

type UnsNodeForceDeleting struct {
	NodeID     int64
	UserID     int64
	DeleteFlow bool
}

type UnsNodeCreated struct {
	Node repo.UnsNode
}

type UnsNodesCreated struct {
	Nodes []repo.UnsNode
}

type UnsNodesUpdated struct {
	Nodes []repo.UnsNode
}

type UnsNodeUpdated struct {
	Node repo.UnsNode
}

type UnsNodeDeleted struct {
	RootID int64
	UserID int64
	Nodes  []repo.UnsNode
}

type UnsNodeRestored struct {
	RootID int64
	UserID int64
	Nodes  []repo.UnsNode
}

// UnsNodesSyncHidden and UnsNodesSyncRestored are visibility-only lifecycle
// events. They intentionally do not trigger the physical cleanup/restore
// outbox used by user recycle-bin actions.
type UnsNodesSyncHidden struct {
	FleetNodeID string
	Nodes       []repo.UnsNode
}

type UnsNodesSyncRestored struct {
	FleetNodeID string
	Nodes       []repo.UnsNode
}

type UnsNodeForceDeleted struct {
	RootID     int64
	UserID     int64
	DeleteFlow bool
	Nodes      []repo.UnsNode
}

package operation

import "context"

const (
	TypeContainerRestart = "container.restart"
	StatusRunning        = "running"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
)

type Record struct {
	OperationID  string
	Type         string
	TargetID     string
	Status       string
	ResultDigest string
	ErrorCode    string
	StartedAt    int64
	FinishedAt   int64
	ExpiresAt    int64
}

type Store interface {
	ClaimOperation(context.Context, Record) (existing Record, created bool, err error)
	CompleteOperation(context.Context, Record) error
}

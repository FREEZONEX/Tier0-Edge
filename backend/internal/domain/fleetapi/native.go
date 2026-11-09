package fleetapi

import (
	"context"

	"backend/internal/domain/unsv2"
)

// ExecuteNative serves the local UNS contract. Edge does not target other nodes.
func (s *Service) ExecuteNative(ctx context.Context, nodeID, operation string, raw []byte) (any, error) {
	if nodeID != "" {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Remote Fleet targets are unavailable in Tier0 Edge"}
	}
	return s.Uns.Execute(ctx, operation, raw)
}

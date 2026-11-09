package mcpserver

import (
	"context"

	"backend/internal/domain/unsv2"
)

// Use the shared compiler policy for collection resource queries and guarded
// writes, including nested lookups. Validation occurs before SQL execution.
func dataQueryReadContext(ctx context.Context) context.Context {
	return unsv2.WithDataQueryFieldPolicy(ctx, unsv2.CheckMCPDataQueryField)
}

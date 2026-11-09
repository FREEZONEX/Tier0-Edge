package mcpserver

import (
	"backend/internal/domain/unsv2"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Deterministic JSON and a content digest avoid mixing contracts across upgrades
// or changed permissions. No unbounded per-client response cache is retained.
func pageContract(value any, input ContractInput) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if input.Offset == nil && input.Limit == 0 && input.SHA256 == "" && len(raw) <= 96<<10 {
		return value, nil
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	if input.SHA256 != "" && input.SHA256 != hash {
		return nil, &unsv2.Fault{Code: "VERSION_CONFLICT", Path: "sha256", Message: "Contract or permissions changed; restart at offset 0 without sha256"}
	}
	offset, limit := 0, input.Limit
	if input.Offset != nil {
		offset = *input.Offset
	}
	if limit == 0 {
		limit = 12000
	}
	chars := []rune(string(raw))
	if offset < 0 || offset > len(chars) || limit < 1 || limit > 24000 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "offset/limit", Message: "Use Unicode offset within totalCharacters and limit 1..24000"}
	}
	end := min(len(chars), offset+limit)
	var next any
	if end < len(chars) {
		next = end
	}
	return map[string]any{"delivery": "pagedJson", "mimeType": "application/json", "text": string(chars[offset:end]), "sha256": hash, "offset": offset, "characters": end - offset, "totalCharacters": len(chars), "sizeBytes": len(raw), "truncated": next != nil, "nextOffset": next, "nextAction": "Concatenate text pages using identical selections and sha256; parse JSON only when truncated=false"}, nil
}

package streamexec

import (
	"bufio"
	"encoding/json"
	"io"
)

// Only bounded JSON crosses this private channel; credentials stay in Backend.
const MaxFrameBytes = 3*MaxJSONBytes + MaxCodeBytes + 64*1024

type LegacySourceCall struct {
	Alias     string          `json:"alias"`
	Options   json.RawMessage `json:"options"`
	TimeoutMs int             `json:"timeoutMs"`
}
type LegacySourceReply struct {
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error,omitempty"`
}
type Frame struct {
	Type   string            `json:"type"`
	Call   *LegacySourceCall `json:"call,omitempty"`
	Result *Result           `json:"result,omitempty"`
}

// FrameReader bounds each allocation, not just the total session length.
type FrameReader struct{ scanner *bufio.Scanner }

func NewFrameReader(r io.Reader) *FrameReader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), MaxFrameBytes)
	return &FrameReader{scanner: s}
}
func (r *FrameReader) Decode(value any) error {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	return json.Unmarshal(r.scanner.Bytes(), value)
}

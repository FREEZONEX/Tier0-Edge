package cluster

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// terminalInputAudit records a bounded, best-effort summary of visibly echoed
// terminal input. It is not shell history or proof that a command was executed.
// Keep this implementation aligned between the Cloud and Enterprise backends.
type terminalInputAudit struct {
	mu        sync.Mutex
	line      []byte
	pending   []string
	output    []byte
	prompt    []byte
	escape    []byte
	osc       bool
	oscEscape bool
	alternate bool
	uncertain bool
	lastCR    bool
	finished  bool
	lines     []string
	bytes     int
	seen      int
	omitted   int
	redacted  int
	truncated bool
}
type terminalInputSummary struct {
	Source             string   `json:"source"`
	ExecutionConfirmed bool     `json:"executionConfirmed"`
	Lines              []string `json:"lines"`
	OmittedLines       int      `json:"omittedLines"`
	RedactedLines      int      `json:"redactedLines"`
	Truncated          bool     `json:"truncated"`
}

const terminalAuditLineLimit = 100
const terminalAuditByteLimit = 16 * 1024
const terminalAuditLineBytes = 1024
const terminalAuditEchoBytes = 8 * 1024

// Deliberately redact the whole line instead of attempting shell-specific
// argument parsing. This does not promise to identify every possible secret.
var terminalAuditSensitive = regexp.MustCompile(`(?i)password|passwd|passphrase|secret|token|authorization|bearer|cookie|credential|private.?key|api.?key|access.?key|sshpass|connectionstring|://[^ /\t]+:[^ /\t]+@|[a-zA-Z0-9_+/=-]{40,}|(^|[ \t])(-p|--password)([ =\t]|$)`)
var terminalAuditPrompt = regexp.MustCompile(`(?i)(password|passphrase|secret|token|verification code|密码|口令|验证码)[^\r\n]*[:：][ \t]*$`)

func (a *terminalInputAudit) Input(data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return
	}
	// An unconfirmed previous line must never be matched against output from
	// a later input (in particular a hidden password followed by a command).
	a.confirm()
	a.omitted += len(a.pending)
	a.pending = nil
	for _, b := range data {
		if b == '\n' && a.lastCR {
			a.lastCR = false
			continue
		}
		a.lastCR = b == '\r'
		if b == '\r' || b == '\n' {
			if len(a.line) > 0 || a.uncertain {
				a.seen++
				line := strings.TrimSpace(string(a.line))
				switch {
				case a.seen > terminalAuditLineLimit:
					a.truncated = true
					a.omitted++
				case a.uncertain || a.alternate || !utf8.Valid(a.line):
					a.omitted++
				case line != "":
					a.pending = append(a.pending, line)
				}
			}
			a.line = nil
			a.uncertain = false
			continue
		}
		if len(a.line) == 0 && !a.uncertain && len(a.pending) == 0 {
			a.output = nil
			if terminalAuditPrompt.Match(a.prompt) {
				a.uncertain = true
			}
		}
		if b < 32 || b == 127 {
			// History, tab completion, cursor editing, interrupts and multiline
			// paste control sequences cannot be reconstructed from stdin alone.
			a.uncertain = true
			continue
		}
		if len(a.line) >= terminalAuditLineBytes {
			a.uncertain = true
			a.truncated = true
			continue
		}
		a.line = append(a.line, b)
	}
	a.confirm()
}
func (a *terminalInputAudit) Output(data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return
	}
	for _, b := range data {
		if a.osc {
			if b == 7 || a.oscEscape && b == '\\' {
				a.osc = false
			}
			a.oscEscape = b == 27
			continue
		}
		if len(a.escape) > 0 {
			a.escape = append(a.escape, b)
			if len(a.escape) == 2 {
				if b == ']' {
					a.osc = true
					a.escape = nil
					continue
				}
				if b != '[' {
					a.escape = nil
				}
				continue
			}
			if b >= 0x40 && b <= 0x7e {
				seq := string(a.escape)
				if seq == "\x1b[?1049h" || seq == "\x1b[?1047h" || seq == "\x1b[?47h" {
					a.alternate = true
					a.uncertain = true
				}
				if seq == "\x1b[?1049l" || seq == "\x1b[?1047l" || seq == "\x1b[?47l" {
					a.alternate = false
				}
				a.escape = nil
			} else if len(a.escape) > 64 {
				a.escape = nil
				a.uncertain = true
			}
			continue
		}
		if b == 27 {
			a.escape = []byte{27}
			continue
		}
		if b == '\r' {
			continue
		}
		if b != '\n' && b < 32 {
			continue
		}
		if b == '\n' {
			a.prompt = nil
		} else {
			a.prompt = append(a.prompt, b)
			if len(a.prompt) > 512 {
				a.prompt = a.prompt[len(a.prompt)-512:]
			}
		}
		if len(a.line) > 0 || len(a.pending) > 0 || a.uncertain {
			a.output = append(a.output, b)
		}
	}
	// Match the echo before retaining only the tail of a large output chunk.
	a.confirm()
	if len(a.output) > terminalAuditEchoBytes {
		a.output = append([]byte(nil), a.output[len(a.output)-terminalAuditEchoBytes:]...)
	}
}
func (a *terminalInputAudit) confirm() {
	for len(a.pending) > 0 {
		line := a.pending[0]
		end := strings.Index(string(a.output), line+"\n")
		if end < 0 {
			return
		}
		a.output = a.output[end+len(line)+1:]
		a.pending = a.pending[1:]
		if terminalAuditSensitive.MatchString(line) {
			line = "[redacted]"
			a.redacted++
		}
		if len(a.lines) >= terminalAuditLineLimit || a.bytes+len(line) > terminalAuditByteLimit {
			a.omitted++
			a.truncated = true
			continue
		}
		a.lines = append(a.lines, line)
		a.bytes += len(line)
	}
}
func (a *terminalInputAudit) Finish() terminalInputSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.confirm()
		a.omitted += len(a.pending)
		if len(a.line) > 0 || a.uncertain {
			a.omitted++
		}
		a.line, a.pending, a.output, a.escape, a.prompt = nil, nil, nil, nil, nil
		a.finished = true
	}
	return terminalInputSummary{Source: "terminalInput", Lines: append([]string{}, a.lines...), OmittedLines: a.omitted, RedactedLines: a.redacted, Truncated: a.truncated}
}

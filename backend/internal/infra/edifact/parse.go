// Package edifact reads the bounded D.96A/UNOW mailbox contract. It never sends
// acknowledgements or performs ERP/business operations.
package edifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBytes = 2 * 1024 * 1024
const MaxSegments = 20000

type Segment struct {
	Tag      string     `json:"tag"`
	Elements [][]string `json:"elements"`
}
type Record struct {
	Kind        string         `json:"kind"`
	Identity    string         `json:"identity"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Sender      string         `json:"sender,omitempty"`
	Recipient   string         `json:"recipient,omitempty"`
	ControlRef  string         `json:"controlRef,omitempty"`
	MessageRef  string         `json:"messageRef,omitempty"`
	MessageType string         `json:"messageType,omitempty"`
	LineNo      string         `json:"lineNo,omitempty"`
	Position    int            `json:"position"`
	Segments    []Segment      `json:"segments,omitempty"`
	ErrorCode   string         `json:"errorCode,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
}

func identity(parts ...string) string { raw, _ := json.Marshal(parts); return string(raw) }
func fingerprint(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func element(s Segment, i, j int) string {
	if i >= len(s.Elements) || j >= len(s.Elements[i]) {
		return ""
	}
	return s.Elements[i][j]
}

// Split at the same time as release processing, so escaped component/element
// delimiters remain literal data. There is no lossy split-then-unescape stage.
func tokenize(data []byte) ([]Segment, error) {
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("invalidEncodingOrSize")
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if bytes.HasPrefix(data, []byte("UNA")) {
		if !bytes.HasPrefix(data, []byte("UNA:+.? '")) {
			return nil, fmt.Errorf("unsupportedUNA")
		}
		data = bytes.TrimSpace(data[9:])
	}
	segments := []Segment{}
	components := []string{}
	fields := [][]string{}
	var text strings.Builder
	escaped := false
	endComponent := func() { components = append(components, text.String()); text.Reset() }
	endField := func() { endComponent(); fields = append(fields, components); components = nil }
	for _, b := range data {
		if escaped {
			if !strings.ContainsRune(":+?' ", rune(b)) {
				return nil, fmt.Errorf("invalidRelease")
			}
			text.WriteByte(b)
			escaped = false
			continue
		}
		switch b {
		case '?':
			escaped = true
		case ':':
			endComponent()
		case '+':
			endField()
		case '\'':
			endField()
			tag := fields[0][0]
			if len(fields[0]) != 1 || len(tag) != 3 || strings.IndexFunc(tag, func(c rune) bool { return c < 'A' || c > 'Z' }) >= 0 {
				return nil, fmt.Errorf("invalidSegmentTag")
			}
			segments = append(segments, Segment{tag, fields[1:]})
			fields = nil
			if len(segments) > MaxSegments {
				return nil, fmt.Errorf("tooManySegments")
			}
		case '\n', '\r':
			if len(fields) != 0 || len(components) != 0 || text.Len() != 0 {
				return nil, fmt.Errorf("newlineInsideSegment")
			}
		default:
			if b < 32 || b == 127 {
				return nil, fmt.Errorf("invalidControlCharacter")
			}
			text.WriteByte(b)
		}
		if len(fields) > 64 || len(components) > 64 || text.Len() > 64*1024 {
			return nil, fmt.Errorf("elementTooLarge")
		}
	}
	if escaped || text.Len() != 0 || len(fields) != 0 || len(components) != 0 {
		return nil, fmt.Errorf("unterminatedSegment")
	}
	return segments, nil
}

func Parse(data []byte) []Record {
	segments, err := tokenize(data)
	if err != nil {
		return []Record{{Kind: "error", ErrorCode: err.Error()}}
	}
	if len(segments) < 2 || segments[0].Tag != "UNB" || segments[len(segments)-1].Tag != "UNZ" {
		return []Record{{Kind: "error", ErrorCode: "missingEnvelope"}}
	}
	head, tail := segments[0], segments[len(segments)-1]
	base := Record{Sender: element(head, 1, 0), Recipient: element(head, 2, 0), ControlRef: element(head, 4, 0)}
	base.Identity = identity(base.Sender, base.Recipient, base.ControlRef)
	fail := func(code string) []Record { r := base; r.Kind = "error"; r.ErrorCode = code; return []Record{r} }
	if element(head, 0, 0) != "UNOW" || element(head, 0, 1) != "4" {
		return fail("unsupportedSyntax")
	}
	if base.Sender == "" || base.Recipient == "" || base.ControlRef == "" {
		return fail("missingEnvelopeIdentity")
	}
	if _, err = time.Parse("200601021504", element(head, 3, 0)+element(head, 3, 1)); err != nil {
		return fail("invalidEnvelopeDate")
	}
	if element(tail, 1, 0) != base.ControlRef {
		return fail("interchangeReferenceMismatch")
	}
	count := 0
	for _, s := range segments[1 : len(segments)-1] {
		if s.Tag == "UNH" {
			count++
		}
		if s.Tag == "UNB" || s.Tag == "UNZ" {
			return fail("nestedEnvelope")
		}
	}
	declared, err := strconv.Atoi(element(tail, 0, 0))
	if err != nil || declared != count {
		return fail("interchangeCountMismatch")
	}
	exchange := base
	exchange.Kind = "exchange"
	exchange.Fingerprint = fingerprint(segments)
	exchange.Segments = []Segment{head, tail}
	records := []Record{exchange}
	seenMessages := map[string]bool{}
	for pos := 1; pos < len(segments)-1; {
		if segments[pos].Tag != "UNH" {
			r := base
			r.Kind = "error"
			r.Position = pos
			r.ErrorCode = "segmentOutsideMessage"
			records = append(records, r)
			pos++
			continue
		}
		end := pos + 1
		for end < len(segments)-1 && segments[end].Tag != "UNH" && segments[end].Tag != "UNT" {
			end++
		}
		if end < len(segments)-1 && segments[end].Tag == "UNT" {
			end++
		}
		reference := element(segments[pos], 0, 0)
		if seenMessages[reference] {
			r := base
			r.Kind = "error"
			r.MessageRef = reference
			r.Position = pos
			r.ErrorCode = "duplicateMessageRef"
			records = append(records, r)
		} else {
			records = append(records, parseMessage(base, segments[pos:end], pos)...)
			seenMessages[reference] = true
		}
		pos = end
	}
	// A corrected malformed interchange must not be pinned as the first valid
	// content. Valid messages inside it remain independently importable.
	for _, r := range records {
		if r.Kind == "error" {
			records[0].Fingerprint = ""
			records[0].ErrorCode = "containsInvalidMessages"
			break
		}
	}
	return records
}

func parseMessage(base Record, segments []Segment, pos int) []Record {
	r := base
	r.Kind = "message"
	r.Position = pos
	r.MessageRef = element(segments[0], 0, 0)
	r.MessageType = element(segments[0], 1, 0)
	r.Identity = identity(r.Sender, r.Recipient, r.ControlRef, r.MessageRef, r.MessageType)
	bad := func(code string) []Record { r.Kind = "error"; r.ErrorCode = code; return []Record{r} }
	allowed := map[string]bool{"ORDERS": true, "ORDRSP": true, "DESADV": true, "INVOIC": true, "CONTRL": true}
	h := segments[0]
	last := segments[len(segments)-1]
	if !allowed[r.MessageType] || element(h, 1, 1) != "D" || element(h, 1, 2) != "96A" || element(h, 1, 3) != "UN" {
		return bad("unsupportedMessage")
	}
	if r.MessageRef == "" || last.Tag != "UNT" || element(last, 1, 0) != r.MessageRef {
		return bad("messageReferenceMismatch")
	}
	n, err := strconv.Atoi(element(last, 0, 0))
	if err != nil || n != len(segments) {
		return bad("messageCountMismatch")
	}
	hasBody := false
	lineNumbers := map[string]bool{}
	for _, s := range segments[1 : len(segments)-1] {
		if s.Tag == "DTM" {
			layout := ""
			switch element(s, 0, 2) {
			case "102":
				layout = "20060102"
			case "203":
				layout = "200601021504"
			default:
				return bad("unsupportedDateFormat")
			}
			if _, err := time.Parse(layout, element(s, 0, 1)); err != nil {
				return bad("invalidMessageDate")
			}
		}
		if r.MessageType != "CONTRL" && s.Tag == "BGM" && element(s, 1, 0) != "" || r.MessageType == "CONTRL" && s.Tag == "UCI" && element(s, 0, 0) != "" {
			hasBody = true
		}
		if s.Tag == "LIN" {
			id := element(s, 0, 0)
			if id == "" || lineNumbers[id] {
				return bad("duplicateOrMissingLineNo")
			}
			lineNumbers[id] = true
		}
		if s.Tag == "UNH" || s.Tag == "UNB" || s.Tag == "UNZ" {
			return bad("nestedMessage")
		}
	}
	if !hasBody {
		return bad("missingMessageBody")
	}
	r.Fingerprint = fingerprint(segments)
	r.Segments = segments
	headerEnd := len(segments)
	for i, s := range segments {
		if s.Tag == "LIN" {
			headerEnd = i
			break
		}
	}
	r.Fields = messageFields(segments[:headerEnd])
	records := []Record{r}
	for i := 1; i < len(segments)-1; i++ {
		if segments[i].Tag != "LIN" {
			continue
		}
		j := i + 1
		for j < len(segments)-1 && segments[j].Tag != "LIN" && segments[j].Tag != "UNS" {
			j++
		}
		line := r
		line.Kind = "line"
		line.LineNo = element(segments[i], 0, 0)
		line.Position = pos + i
		line.Segments = segments[i:j]
		line.Fields = messageFields(line.Segments)
		line.Identity = identity(line.Sender, line.Recipient, line.ControlRef, line.MessageRef, line.MessageType, line.LineNo)
		line.Fingerprint = fingerprint(line.Segments)
		records = append(records, line)
		i = j - 1
	}
	return records
}

// Numeric and identifier values remain source strings, including leading zeros,
// decimal scale and empty components. The raw segments retain every qualifier.
func messageFields(segments []Segment) map[string]any {
	fields := map[string]any{}
	for _, segment := range segments {
		switch segment.Tag {
		case "BGM":
			fields["documentType"], fields["documentNo"], fields["functionCode"] = element(segment, 0, 0), element(segment, 1, 0), element(segment, 2, 0)
		case "LIN":
			fields["lineNo"], fields["actionCode"], fields["itemCode"], fields["itemQualifier"] = element(segment, 0, 0), element(segment, 1, 0), element(segment, 2, 0), element(segment, 2, 1)
		case "UCI":
			fields["referencedControlRef"], fields["actionCode"], fields["syntaxErrorCode"] = element(segment, 0, 0), element(segment, 3, 0), element(segment, 4, 0)
		case "DTM", "QTY", "PRI", "MOA", "RFF", "PIA", "NAD", "UCM", "GIN", "CUX", "FTX", "IMD":
			key := strings.ToLower(segment.Tag)
			values, _ := fields[key].([][][]string)
			fields[key] = append(values, segment.Elements)
		}
	}
	return fields
}

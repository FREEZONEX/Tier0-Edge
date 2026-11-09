// source-stream-single-model creates a reviewed draft candidate offline. It has
// no database connection, release update, runtime fallback or execution path.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"backend/internal/domain/sourcestream"
)

const maxDraftBytes = 1024 * 1024

func convertDraft(kind string, raw []byte) ([]byte, error) {
	if len(raw) > maxDraftBytes {
		return nil, errors.New("draft too large")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("a bare draft document is required")
	}
	// Only the known complete-program label can be removed automatically.
	// Transform bodies and unknown model labels require explicit offline review.
	if value, ok := fields["sdkVersion"]; ok {
		var label string
		if kind != "stream" || json.Unmarshal(value, &label) != nil || label != "tier0-stream/v2" {
			return nil, errors.New("unknown model selector requires manual review")
		}
		delete(fields, "sdkVersion")
	}
	for _, field := range []string{"configVersion", "supportedVersions"} {
		if _, ok := fields[field]; ok {
			return nil, errors.New("unrecognized selector requires manual review: " + field)
		}
	}
	candidate, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "stream":
		var draft sourcestream.StreamDefinition
		if err := json.Unmarshal(candidate, &draft); err != nil {
			return nil, err
		}
		if err := draft.ValidateDraft(); err != nil {
			return nil, err
		}
	case "source":
		// Reuse the production shape and secret-material checks without I/O.
		var input any
		decoder := json.NewDecoder(bytes.NewReader(candidate))
		decoder.UseNumber()
		if err := decoder.Decode(&input); err != nil {
			return nil, err
		}
		if err := sourcestream.New(nil, nil, "").ValidateSource(input); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("kind must be source or stream")
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, candidate, "", "  "); err != nil {
		return nil, err
	}
	return append(formatted.Bytes(), '\n'), nil
}

func run() error {
	kind := flag.String("kind", "", "source or stream; bare draft documents only")
	input := flag.String("input", "", "read-only input file; never a release or database snapshot")
	output := flag.String("output", "", "new candidate file; must not already exist")
	flag.Parse()
	if *input == "" || *output == "" || flag.NArg() != 0 {
		return errors.New("-kind, -input and -output are required")
	}
	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxDraftBytes+1))
	if err != nil {
		return err
	}
	candidate, err := convertDraft(*kind, raw)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := out.Write(candidate)
	closeErr := out.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	before, after := sha256.Sum256(raw), sha256.Sum256(candidate)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"kind": *kind, "candidate": *output, "inputSHA256": hex.EncodeToString(before[:]), "candidateSHA256": hex.EncodeToString(after[:]),
		"shapeValidated": true, "scriptExecuted": false, "cutoverReady": false,
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

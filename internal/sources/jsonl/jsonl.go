// Package jsonl reads the shelf contract written out as a file: one JSON
// object per line, each holding either an item or an event.
//
//	{"item": {"id": "1", "title": "Dune", "authors": ["Frank Herbert"], "format": "print"}}
//	{"event": {"item_id": "1", "kind": "finished", "on": "2021-05-03", "date_quality": "user"}}
//
// Field names are the JSON tags on contract.ShelfItem and contract.ShelfEvent.
// This is the zero-code way to add a source: any script that writes this
// format can be ingested with `recbook ingest jsonl --as <name> file`. The
// demo data in testdata/ uses it too.
package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// line is one record. Exactly one of its fields must be set.
type line struct {
	Item  *contract.ShelfItem  `json:"item"`
	Event *contract.ShelfEvent `json:"event"`
}

// Read parses a JSONL shelf. Blank lines are skipped; unknown fields are an
// error, so a misspelled field name doesn't silently drop data.
func Read(r io.Reader) (*contract.Shelf, error) {
	shelf := &contract.Shelf{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // descriptions can make long lines

	for n := 1; scanner.Scan(); n++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var l line
		if err := dec.Decode(&l); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		switch {
		case l.Item != nil && l.Event == nil:
			shelf.Items = append(shelf.Items, *l.Item)
		case l.Event != nil && l.Item == nil:
			shelf.Events = append(shelf.Events, *l.Event)
		default:
			return nil, fmt.Errorf(`line %d: want exactly one of "item" or "event"`, n)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return shelf, nil
}

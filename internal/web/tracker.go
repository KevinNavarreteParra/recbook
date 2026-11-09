package web

import (
	"bytes"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
)

// Tracker records what I mark in the app: listening, finished, gave up,
// want. It's a shelf source like any other: its whole state is a shelf in
// the contract's JSONL format (data/tracker.jsonl), which the nightly job
// ingests as source "tracker", so marks flow into labels and tiers. It is
// also the only source that records giving up on a book.
//
// Items are catalog recordings keyed by their catalog id, with an
// OverDrive ref so they match the catalog exactly.
type Tracker struct {
	mu    sync.Mutex
	path  string
	shelf contract.Shelf
	now   func() time.Time
}

// Marks a tracker accepts, in the order the app offers them.
var marks = []contract.EventKind{contract.Started, contract.Finished, contract.Abandoned, contract.Wanted}

// OpenTracker loads the tracker file at path, or starts empty if there
// isn't one yet.
func OpenTracker(path string) (*Tracker, error) {
	t := &Tracker{path: path, now: time.Now}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	shelf, err := jsonl.Read(f)
	if err != nil {
		return nil, err
	}
	t.shelf = *shelf
	return t, nil
}

// Mark records kind for item today, adding the item if it's new.
func (t *Tracker) Mark(item contract.ShelfItem, kind contract.EventKind) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	next := contract.Shelf{Items: slices.Clone(t.shelf.Items), Events: slices.Clone(t.shelf.Events)}
	if !slices.ContainsFunc(next.Items, func(it contract.ShelfItem) bool { return it.ID == item.ID }) {
		next.Items = append(next.Items, item)
	}
	next.Events = append(next.Events, contract.ShelfEvent{
		ItemID: item.ID, Kind: kind, On: contract.DayOf(t.now()), DateQuality: contract.DateSystem,
	})
	return t.save(next)
}

// Undo removes the latest mark on an item, and the item once it has none.
func (t *Tracker) Undo(itemID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	next := contract.Shelf{Items: slices.Clone(t.shelf.Items), Events: slices.Clone(t.shelf.Events)}
	last := -1
	for i, ev := range next.Events {
		if ev.ItemID == itemID {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	next.Events = slices.Delete(next.Events, last, last+1)
	if !slices.ContainsFunc(next.Events, func(ev contract.ShelfEvent) bool { return ev.ItemID == itemID }) {
		next.Items = slices.DeleteFunc(next.Items, func(it contract.ShelfItem) bool { return it.ID == itemID })
	}
	return t.save(next)
}

// Status is an item's latest mark.
type Status struct {
	Kind contract.EventKind
	On   contract.Day
}

// Statuses returns each marked item's latest mark, by item id.
func (t *Tracker) Statuses() map[string]Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]Status{}
	for _, ev := range t.shelf.Events {
		out[ev.ItemID] = Status{ev.Kind, ev.On}
	}
	return out
}

// Entry is one mark, for the history page.
type Entry struct {
	Item contract.ShelfItem
	Status
}

// History returns every mark, newest first.
func (t *Tracker) History() []Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	items := map[string]contract.ShelfItem{}
	for _, it := range t.shelf.Items {
		items[it.ID] = it
	}
	var out []Entry
	for i := len(t.shelf.Events) - 1; i >= 0; i-- {
		ev := t.shelf.Events[i]
		out = append(out, Entry{Item: items[ev.ItemID], Status: Status{ev.Kind, ev.On}})
	}
	return out
}

// save validates next, writes it atomically, and only then makes it the
// tracker's state, so a failed write changes nothing.
func (t *Tracker) save(next contract.Shelf) error {
	if err := next.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := jsonl.Write(&buf, &next); err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return err
	}
	t.shelf = next
	return nil
}

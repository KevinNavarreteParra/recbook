package backlog

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// TestList runs the whole pipeline on the demo: ingest, match, signals,
// backlog.
func TestList(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := library.Build(ctx, st); err != nil {
		t.Fatal(err)
	}

	titles := func(opts Options) []string {
		t.Helper()
		entries, err := List(ctx, st, opts)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			out = append(out, e.Title)
		}
		return out
	}

	all := titles(Options{MaxWaitDays: -1})
	want := []string{
		// available now: owned (T4) before wanted (T5)
		"Ethics", "The Brothers Karamazov", "The Spy Who Came in from the Cold",
		// then by wait
		"Meditations", "A Gentleman in Moscow", "The Republic", "Gone Girl",
	}
	if !slices.Equal(all, want) {
		t.Errorf("backlog =\n  %q\nwant\n  %q", all, want)
	}
	// Notes from Underground is owned and unread, but the library lacks it;
	// the two Karamazov recordings collapse into the available one.

	// A Gentleman in Moscow has a dramatized version available now, but the
	// full recording wins despite its wait. Karamazov's "Dramatised" one
	// loses to the available full recording.
	entries, err := List(ctx, st, Options{MaxWaitDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Title == "A Gentleman in Moscow" && (e.Abridged || !strings.HasSuffix(e.URL, "/9011")) {
			t.Errorf("want the full recording 9011, got %s (abridged=%v)", e.URL, e.Abridged)
		}
		if e.Title == "The Brothers Karamazov" && !strings.HasSuffix(e.URL, "/9010") {
			t.Errorf(`"Dramatised" (British spelling) is not the full text: got %s`, e.URL)
		}
	}

	if got := titles(Options{AvailableNow: true, MaxWaitDays: -1}); len(got) != 3 {
		t.Errorf("--available-now = %q", got)
	}
	if got := titles(Options{MaxWaitDays: 20}); len(got) != 4 || got[3] != "Meditations" {
		t.Errorf("--max-wait 20 = %q", got)
	}
	if got := titles(Options{MaxWaitDays: -1, Limit: 2}); len(got) != 2 {
		t.Errorf("--limit 2 = %q", got)
	}
}

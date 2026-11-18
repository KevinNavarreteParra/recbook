package library

import (
	"context"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func TestBuildPicksOneRecordingPerWork(t *testing.T) {
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
	if err := Build(ctx, st, nil); err != nil {
		t.Fatal(err)
	}

	tests := []struct{ title, wantID string }{
		{"The Brothers Karamazov", "9010"}, // available full text, over a waitlisted one and a dramatised one
		{"A Gentleman in Moscow", "9011"},  // full text with a wait, over a dramatized one available now
	}
	for _, tt := range tests {
		var id string
		var recordings int
		err := st.DB().QueryRow(`SELECT title_id, recordings FROM borrowable WHERE title = ?`, tt.title).Scan(&id, &recordings)
		if err != nil {
			t.Fatalf("%s: %v", tt.title, err)
		}
		if id != tt.wantID || recordings < 2 {
			t.Errorf("%s: offered %s of %d recordings, want %s", tt.title, id, recordings, tt.wantID)
		}
	}
}

func TestBuildPrefersMyLanguages(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	// A Spanish recording of a demo book, catalogued under the same title:
	// available, and longer than the English one, so with no language
	// preference it's the one offered.
	titles, avail := demo.Catalog()
	spanish := titles[0]
	for _, ti := range titles {
		if ti.ID == "9010" {
			spanish = ti
		}
	}
	spanish.ID, spanish.Language, spanish.DurationMin = "9030", "es", 900
	titles = append(titles, spanish)
	avail = append(avail, contract.Availability{TitleID: "9030", Available: true, CopiesOwned: 1, CopiesAvailable: 1})
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		languages    []string
		wantID, lang string
	}{
		{nil, "9030", "es"},                  // any language: the longest
		{[]string{"en"}, "9010", "en"},       // mine, over a longer one in another language
		{[]string{"es", "en"}, "9030", "es"}, // the first of mine
		{[]string{"fr"}, "9030", "es"},       // none in mine: offered anyway, in what there is
	}
	for _, tt := range tests {
		if err := Build(ctx, st, tt.languages); err != nil {
			t.Fatal(err)
		}
		var id, lang string
		err := st.DB().QueryRow(`SELECT title_id, language FROM borrowable WHERE title = 'The Brothers Karamazov'`).Scan(&id, &lang)
		if err != nil {
			t.Fatal(err)
		}
		if id != tt.wantID || lang != tt.lang {
			t.Errorf("languages %v: offered %s (%s), want %s (%s)", tt.languages, id, lang, tt.wantID, tt.lang)
		}
	}
}

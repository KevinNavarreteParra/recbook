package contract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeISBN(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"9780140449136", "9780140449136", true},     // ISBN-13
		{"978-0-14-044913-6", "9780140449136", true}, // hyphenated
		{"0140449132", "9780140449136", true},        // ISBN-10 converts
		{"0-8044-2957-X", "9780804429573", true},     // X check digit
		{"080442957x", "9780804429573", true},        // lowercase x
		{"9780140449137", "", false},                 // wrong check digit
		{"0140449133", "", false},                    // wrong ISBN-10 check digit
		{"12345", "", false},                         // wrong length
		{`="0140449132"`, "", false},                 // spreadsheet junk is the adapter's job
	}
	for _, tt := range tests {
		got, err := NormalizeISBN(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("NormalizeISBN(%q) = %q, %v; want %q, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestDayJSON(t *testing.T) {
	type wrap struct {
		On Day `json:"on,omitzero"`
	}

	b, err := json.Marshal(wrap{On: NewDay(2024, time.March, 1)})
	if err != nil || string(b) != `{"on":"2024-03-01"}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	if b, _ := json.Marshal(wrap{}); string(b) != `{}` {
		t.Errorf("unknown day should be omitted, got %s", b)
	}

	for _, in := range []string{`{"on":""}`, `{"on":null}`, `{}`} {
		var w wrap
		if err := json.Unmarshal([]byte(in), &w); err != nil || !w.On.IsZero() {
			t.Errorf("unmarshal %s = %v, %v; want unknown day", in, w.On, err)
		}
	}

	var w wrap
	if err := json.Unmarshal([]byte(`{"on":"2024-03-01T10:00:00Z"}`), &w); err == nil {
		t.Error("a timestamp is not a day; want an error")
	}
}

func validShelf() *Shelf {
	return &Shelf{
		Items: []ShelfItem{{
			ID: "1", Title: "Tinker Tailor Soldier Spy", Authors: []string{"John le Carré"},
			ISBNs: []string{"0-14-044913-2"}, Format: Print, Refs: map[string]string{RefGoodreads: "18989"},
		}},
		Events: []ShelfEvent{
			{ItemID: "1", Kind: Finished, On: NewDay(2023, time.April, 2), DateQuality: DateUser},
			{ItemID: "1", Kind: Acquired}, // undated is fine
			{ItemID: "1", Kind: Returned, On: NewDay(2023, time.May, 1), DateQuality: DateSystem, Detail: ReturnedEarly},
		},
	}
}

func TestValidateAcceptsValidShelf(t *testing.T) {
	if err := validShelf().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Shelf)
		want   string
	}{
		{"duplicate id", func(s *Shelf) { s.Items = append(s.Items, s.Items[0]) }, "used by an earlier item"},
		{"no author", func(s *Shelf) { s.Items[0].Authors = nil }, "at least one author"},
		{"bad format", func(s *Shelf) { s.Items[0].Format = "hardcover" }, "format must be one of"},
		{"bad isbn", func(s *Shelf) { s.Items[0].ISBNs = []string{"123"} }, `ISBN "123"`},
		{"unknown ref", func(s *Shelf) { s.Items[0].Refs = map[string]string{"amazon": "x"} }, `ref key "amazon"`},
		{"orphan event", func(s *Shelf) { s.Events[0].ItemID = "nope" }, "matches no item"},
		{"bad kind", func(s *Shelf) { s.Events[0].Kind = "loved" }, "kind must be one of"},
		{"date without quality", func(s *Shelf) { s.Events[0].DateQuality = "" }, "needs date_quality"},
		{"detail on wrong kind", func(s *Shelf) { s.Events[0].Detail = "early" }, "only returned events"},
		{"bad return detail", func(s *Shelf) { s.Events[2].Detail = "lost" }, "detail must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validShelf()
			tt.mutate(s)
			err := s.Validate()
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("want *InvalidError, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q doesn't mention %q", err, tt.want)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	s := &Shelf{}
	for range 30 {
		s.Items = append(s.Items, ShelfItem{}) // each one breaks several rules
	}
	err := s.Validate()
	var inv *InvalidError
	if !errors.As(err, &inv) || len(inv.Problems) < 30 {
		t.Fatalf("want every problem collected, got %v", err)
	}
	if !strings.Contains(err.Error(), "more") {
		t.Error("long problem lists should be truncated in the message")
	}
}

func TestValidateCatalog(t *testing.T) {
	wait := 14
	titles := []CatalogTitle{{
		ID: "5700929", Title: "Etheric Explorer", Format: Audiobook,
		Creators: []Creator{{Name: "S.R. Russell", Role: Author}, {Name: "Mare Trevathan", Role: Narrator}},
	}}
	avail := []Availability{{TitleID: "5700929", CopiesOwned: 1, Holds: 3, WaitDays: &wait}}
	if err := ValidateCatalog(titles, avail); err != nil {
		t.Fatal(err)
	}

	titles[0].Creators[1].Role = "reader"
	avail = append(avail, Availability{TitleID: "missing"})
	err := ValidateCatalog(titles, avail)
	if err == nil || !strings.Contains(err.Error(), "role must be") || !strings.Contains(err.Error(), "matches no title") {
		t.Errorf("want role and orphan-availability problems, got %v", err)
	}
}

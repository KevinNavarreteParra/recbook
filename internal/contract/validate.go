package contract

import (
	"fmt"
	"slices"
	"strings"
)

// Validate reports every way the shelf breaks the contract, all at once, so
// an adapter author can fix them in one pass. It returns nil when the shelf
// is valid.
func (s *Shelf) Validate() error {
	var p problems

	ids := make(map[string]bool, len(s.Items))
	for i, it := range s.Items {
		at := fmt.Sprintf("item %d (id %q)", i, it.ID)
		switch {
		case it.ID == "":
			p.add(at, "id is empty")
		case ids[it.ID]:
			p.add(at, "id is used by an earlier item")
		}
		ids[it.ID] = true

		if strings.TrimSpace(it.Title) == "" {
			p.add(at, "title is empty")
		}
		if len(it.Authors) == 0 || strings.TrimSpace(it.Authors[0]) == "" {
			p.add(at, "needs at least one author (use \"Unknown\" if the source has none)")
		}
		checkFormat(&p, at, it.Format)
		checkISBNs(&p, at, it.ISBNs)
		checkRefs(&p, at, it.Refs)
	}

	for i, ev := range s.Events {
		at := fmt.Sprintf("event %d (item %q, %s)", i, ev.ItemID, ev.Kind)
		if !ids[ev.ItemID] {
			p.add(at, "item_id matches no item")
		}
		if !slices.Contains(EventKinds, ev.Kind) {
			p.add(at, fmt.Sprintf("kind must be one of %v", EventKinds))
		}
		if !ev.On.IsZero() && ev.DateQuality != DateSystem && ev.DateQuality != DateUser {
			p.add(at, "a dated event needs date_quality \"system\" or \"user\"")
		}
		switch {
		case ev.Kind == Returned:
			if ev.Detail != "" && ev.Detail != ReturnedEarly && ev.Detail != ReturnedExpired {
				p.add(at, fmt.Sprintf("detail must be %q, %q or empty", ReturnedEarly, ReturnedExpired))
			}
		case ev.Detail != "":
			p.add(at, "only returned events take a detail")
		}
	}

	return p.err("shelf")
}

// ValidateCatalog reports every way a batch of catalog titles and their
// availability breaks the contract. It returns nil when the batch is valid.
func ValidateCatalog(titles []CatalogTitle, avail []Availability) error {
	var p problems

	ids := make(map[string]bool, len(titles))
	for i, t := range titles {
		at := fmt.Sprintf("title %d (id %q)", i, t.ID)
		switch {
		case t.ID == "":
			p.add(at, "id is empty")
		case ids[t.ID]:
			p.add(at, "id is used by an earlier title")
		}
		ids[t.ID] = true

		if strings.TrimSpace(t.Title) == "" {
			p.add(at, "title is empty")
		}
		for _, c := range t.Creators {
			if c.Name == "" {
				p.add(at, "a creator has no name")
			}
			if !slices.Contains(Roles, c.Role) {
				p.add(at, fmt.Sprintf("creator %q: role must be one of %v", c.Name, Roles))
			}
		}
		checkFormat(&p, at, t.Format)
		checkISBNs(&p, at, t.ISBNs)
		checkRefs(&p, at, t.Refs)
		if t.DurationMin < 0 {
			p.add(at, "duration is negative")
		}
	}

	for i, a := range avail {
		at := fmt.Sprintf("availability %d (title %q)", i, a.TitleID)
		if !ids[a.TitleID] {
			p.add(at, "title_id matches no title in the batch")
		}
		checkAvailability(&p, at, a)
	}

	return p.err("catalog batch")
}

// ValidateAvailability reports every way a batch of availability snapshots
// for titles already in the catalog breaks the contract. It returns nil
// when the batch is valid.
func ValidateAvailability(avail []Availability) error {
	var p problems
	for i, a := range avail {
		at := fmt.Sprintf("availability %d (title %q)", i, a.TitleID)
		if a.TitleID == "" {
			p.add(at, "title_id is empty")
		}
		checkAvailability(&p, at, a)
	}
	return p.err("availability batch")
}

func checkAvailability(p *problems, at string, a Availability) {
	if a.CopiesOwned < 0 || a.CopiesAvailable < 0 || a.Holds < 0 {
		p.add(at, "counts can't be negative")
	}
	if a.WaitDays != nil && *a.WaitDays < 0 {
		p.add(at, "wait estimate can't be negative")
	}
}

func checkFormat(p *problems, at string, f Format) {
	if !slices.Contains(Formats, f) {
		p.add(at, fmt.Sprintf("format must be one of %v", Formats))
	}
}

func checkISBNs(p *problems, at string, isbns []string) {
	for _, s := range isbns {
		if _, err := NormalizeISBN(s); err != nil {
			p.add(at, fmt.Sprintf("ISBN %q: %v", s, err))
		}
	}
}

func checkRefs(p *problems, at string, refs map[string]string) {
	for k, v := range refs {
		if !slices.Contains(RefKeys, k) {
			p.add(at, fmt.Sprintf("ref key %q must be one of %v", k, RefKeys))
		}
		if v == "" {
			p.add(at, fmt.Sprintf("ref %q is empty", k))
		}
	}
}

// problems collects contract violations so they can be reported together.
type problems []string

func (p *problems) add(where, what string) {
	*p = append(*p, where+": "+what)
}

// maxShown caps how many problems an error lists; a broken adapter can
// produce thousands, and the first few are enough to see the pattern.
const maxShown = 20

func (p problems) err(what string) error {
	if len(p) == 0 {
		return nil
	}
	shown := p
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}
	msg := fmt.Sprintf("%s breaks the contract in %d places:\n  %s", what, len(p), strings.Join(shown, "\n  "))
	if len(p) > maxShown {
		msg += fmt.Sprintf("\n  ... and %d more", len(p)-maxShown)
	}
	return &InvalidError{Problems: p, msg: msg}
}

// InvalidError is returned by Validate and ValidateCatalog. Problems holds
// every violation, one line each.
type InvalidError struct {
	Problems []string
	msg      string
}

func (e *InvalidError) Error() string { return e.msg }

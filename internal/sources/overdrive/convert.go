package overdrive

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// toContract translates one Thunder record into the catalog contract.
func (c *Client) toContract(m media) (contract.CatalogTitle, contract.Availability) {
	t := contract.CatalogTitle{
		ID:          m.ID,
		Title:       strings.TrimSpace(m.Title),
		Subtitle:    strings.TrimSpace(m.Subtitle),
		Format:      formatOf(m.Type.ID),
		Edition:     m.Edition,
		Publisher:   m.Publisher.Name,
		Description: m.Description,
		Series:      m.Series,
		Refs:        map[string]string{contract.RefOverDrive: m.ID},
		URL:         fmt.Sprintf("https://libbyapp.com/library/%s/everything/page-1/%s", c.cfg.Library, m.ID),
	}

	for _, cr := range m.Creators {
		t.Creators = append(t.Creators, contract.Creator{Name: cr.Name, Role: roleOf(cr.Role)})
	}

	seen := map[string]bool{}
	for _, f := range m.Formats {
		// Thunder occasionally carries a malformed ISBN; the contract only
		// takes valid ones.
		if isbn, err := contract.NormalizeISBN(f.ISBN); err == nil && !seen[isbn] {
			seen[isbn] = true
			t.ISBNs = append(t.ISBNs, isbn)
		}
		if t.DurationMin == 0 {
			t.DurationMin = minutes(f.Duration)
		}
	}

	if len(m.Languages) > 0 {
		t.Language = m.Languages[0].ID
	}
	if pub, err := time.Parse(time.RFC3339, m.PublishDate); err == nil {
		t.PublishedOn = contract.DayOf(pub)
	}
	for _, s := range m.Subjects {
		t.Subjects = append(t.Subjects, contract.Subject{Scheme: "overdrive", Code: s.ID, Name: s.Name})
	}
	for _, b := range m.Bisac {
		t.Subjects = append(t.Subjects, contract.Subject{Scheme: "bisac", Code: b.Code, Name: strings.TrimSpace(b.Description)})
	}
	if s := m.DetailedSeries; s != nil {
		if t.Series == "" {
			t.Series = s.SeriesName
		}
		t.SeriesIndex = s.ReadingOrder
	}
	if cover, ok := m.Covers["cover300Wide"]; ok {
		t.CoverURL = cover.Href
	}

	return t, availabilityOf(m)
}

// availabilityOf reads a record's availability, which list results and
// bulk results both carry in full.
func availabilityOf(m media) contract.Availability {
	a := contract.Availability{
		TitleID:         m.ID,
		Available:       m.IsAvailable,
		CopiesOwned:     max(m.OwnedCopies, 0),
		CopiesAvailable: max(m.AvailableCopies, 0),
		Holds:           max(m.HoldsCount, 0),
	}
	if w := m.EstimatedWaitDays; w != nil && *w >= 0 {
		a.WaitDays = w
	}
	return a
}

func formatOf(typeID string) contract.Format {
	switch typeID {
	case "audiobook":
		return contract.Audiobook
	case "ebook":
		return contract.Ebook
	default:
		return contract.UnknownFormat
	}
}

func roleOf(role string) contract.Role {
	switch strings.ToLower(role) {
	case "author":
		return contract.Author
	case "narrator":
		return contract.Narrator
	case "translator":
		return contract.Translator
	case "editor":
		return contract.Editor
	default:
		return contract.OtherRole
	}
}

// minutes parses a Thunder duration, "HH:MM:SS", rounding to the nearest
// minute. It returns 0 if the duration is missing or malformed.
func minutes(hms string) int {
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return 0
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0
		}
		n[i] = v
	}
	return n[0]*60 + n[1] + (n[2]+30)/60
}

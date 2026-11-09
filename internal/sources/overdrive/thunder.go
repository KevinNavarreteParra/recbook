// Package overdrive is the catalog source: it reads a library's audiobook
// collection from OverDrive's public Thunder API (the one the Libby app
// uses) and turns it into contract.CatalogTitle records.
//
// Thunder is undocumented, so everything about it is confined to this
// package. What we know, from probing it in November 2026:
//
//   - GET /v2/libraries/{key}/media lists titles, at most 100 per page, and
//     stops paging at page 200. One query therefore reaches at most 20,000
//     titles; bigger collections must be split into smaller queries (see
//     crawl.go).
//   - includeFacets=false drops the facet counts, shrinking a page from
//     ~600 KB to a few KB.
//   - List results cut descriptions short (~250 characters).
//     GET /v2/libraries/{key}/media/bulk?titleIds=a,b,... returns full
//     records for up to 100 ids at a time.
package overdrive

import (
	"encoding/json"
	"fmt"
)

// The JSON shapes below cover only the fields recbook uses. Thunder sends
// many more.

// searchResult is one page of GET /media.
type searchResult struct {
	Items      []media `json:"items"`
	TotalItems int     `json:"totalItems"`

	// Facets is left raw because not every entry is a facet (there is an
	// "appliedCount" number among them); facet decodes the one asked for.
	Facets map[string]json.RawMessage `json:"facets"`
}

// facet returns the values of one facet and how many titles each has.
func (r *searchResult) facet(name string) ([]facetValue, error) {
	raw, ok := r.Facets[name]
	if !ok {
		return nil, nil
	}
	var set struct {
		Items []facetValue `json:"items"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("facet %s: %w", name, err)
	}
	return set.Items, nil
}

type facetValue struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TotalItems int    `json:"totalItems"`
}

// bulkResult is the response of GET /media/bulk.
type bulkResult struct {
	Items []media `json:"items"`
}

// media is one title.
type media struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"` // HTML; truncated in list results
	Edition     string `json:"edition"`
	PublishDate string `json:"publishDate"` // RFC 3339
	Series      string `json:"series"`

	Type struct {
		ID string `json:"id"` // "audiobook", "ebook", ...
	} `json:"type"`

	Creators []struct {
		Name string `json:"name"`
		Role string `json:"role"` // "Author", "Narrator", "Translator", ...
	} `json:"creators"`

	Formats []struct {
		ID       string `json:"id"`       // "audiobook-overdrive", "audiobook-mp3"
		ISBN     string `json:"isbn"`     // may be empty
		Duration string `json:"duration"` // "03:21:16"
	} `json:"formats"`

	Languages []struct {
		ID string `json:"id"` // "en"
	} `json:"languages"`

	Publisher struct {
		Name string `json:"name"`
	} `json:"publisher"`

	Subjects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"subjects"`

	Bisac []struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	} `json:"bisac"`

	DetailedSeries *struct {
		SeriesName   string `json:"seriesName"`
		ReadingOrder string `json:"readingOrder"`
	} `json:"detailedSeries"`

	Covers map[string]struct {
		Href string `json:"href"`
	} `json:"covers"`

	IsAvailable       bool `json:"isAvailable"`
	OwnedCopies       int  `json:"ownedCopies"`
	AvailableCopies   int  `json:"availableCopies"`
	HoldsCount        int  `json:"holdsCount"`
	EstimatedWaitDays *int `json:"estimatedWaitDays"`
}

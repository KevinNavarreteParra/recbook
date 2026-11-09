package overdrive

import (
	"context"
	"fmt"
	"maps"
	"net/url"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// Batch is one page of crawled titles, ready for store.SaveCatalog.
type Batch struct {
	Titles       []contract.CatalogTitle
	Availability []contract.Availability
}

// Progress reports how far a crawl has got, for printing.
type Progress struct {
	Partition, Partitions int // which query, of how many
	Page, Pages           int // which page of that query
	Saved                 int // distinct titles saved so far
}

// Result summarizes a finished crawl.
type Result struct {
	Listed  int    // titles the library says it has
	Saved   int    // distinct titles actually saved
	Warning string // set when titles were missed
}

// Crawl reads the library's whole collection, passing each page to save as
// it goes, so an interrupted crawl keeps what it fetched. progress, if not
// nil, is called after every page.
//
// Thunder serves only the first MaxPages pages of a query, so a collection
// bigger than PageSize × MaxPages titles is first split into partitions
// small enough to page through (see plan).
func (c *Client) Crawl(ctx context.Context, save func(Batch) error, progress func(Progress)) (Result, error) {
	var res Result

	top, err := c.search(ctx, url.Values{}, 1, 1, false)
	if err != nil {
		return res, err
	}
	res.Listed = top.TotalItems

	parts, err := c.plan(ctx, url.Values{}, 0)
	if err != nil {
		return res, err
	}

	// Subject partitions overlap (a title can have several subjects), so
	// skip any title an earlier partition already saved.
	saved := map[string]bool{}

	for i, p := range parts {
		pages := min((p.total+c.cfg.PageSize-1)/c.cfg.PageSize, c.cfg.MaxPages)
		for page := 1; page <= pages; page++ {
			listing, err := c.search(ctx, p.filters, page, c.cfg.PageSize, false)
			if err != nil {
				return res, err
			}
			if len(listing.Items) == 0 {
				break // the partition shrank since it was planned
			}

			var ids []string
			for _, m := range listing.Items {
				if !saved[m.ID] {
					ids = append(ids, m.ID)
				}
			}
			if len(ids) > 0 {
				// List results truncate descriptions; bulk has the full text.
				full, err := c.bulk(ctx, ids)
				if err != nil {
					return res, err
				}
				var b Batch
				for _, m := range full {
					if saved[m.ID] {
						continue
					}
					t, a := c.toContract(m)
					b.Titles = append(b.Titles, t)
					b.Availability = append(b.Availability, a)
				}
				if err := save(b); err != nil {
					return res, err
				}
				for _, t := range b.Titles {
					saved[t.ID] = true
				}
			}

			res.Saved = len(saved)
			if progress != nil {
				progress(Progress{Partition: i + 1, Partitions: len(parts), Page: page, Pages: pages, Saved: res.Saved})
			}
		}
	}

	// The only reliable coverage check: subject partitions overlap, so an
	// oversized one can still be covered by its neighbours.
	if res.Saved < res.Listed {
		res.Warning = fmt.Sprintf(
			"saved %d of the %d titles listed; the rest were added mid-crawl or no partition reached them",
			res.Saved, res.Listed)
	}
	return res, nil
}

// partition is a query small enough for Thunder to page through completely.
type partition struct {
	filters url.Values
	total   int
}

// splits are the facets used, in order, to cut an oversized query into
// partitions: each facet value becomes a narrower query. Duration and
// maturity buckets don't overlap and together cover every title, so they
// split cleanly. Subjects overlap, so they come last.
var splits = []struct {
	facet string // key in the response's facets
	param string // query parameter that filters on it
}{
	{"audiobookDuration", "audiobookDuration"},
	{"maturityLevels", "maturityLevel"},
	{"subjects", "subject"},
}

// plan returns partitions that together cover filters' results, splitting
// on splits[depth:] for as long as a query is too big to page through. A
// query still too big after every split is crawled as far as Thunder
// allows.
func (c *Client) plan(ctx context.Context, filters url.Values, depth int) ([]partition, error) {
	limit := c.cfg.PageSize * c.cfg.MaxPages

	res, err := c.search(ctx, filters, 1, 1, true)
	if err != nil {
		return nil, err
	}
	if res.TotalItems <= limit || depth == len(splits) {
		return []partition{{filters, min(res.TotalItems, limit)}}, nil
	}

	split := splits[depth]
	values, err := res.facet(split.facet)
	if err != nil {
		return nil, err
	}
	var parts []partition
	for _, v := range values {
		if v.TotalItems == 0 {
			continue
		}
		child := maps.Clone(filters)
		child.Set(split.param, v.ID)
		if v.TotalItems <= limit {
			// The facet count already says it fits; no need to ask again.
			parts = append(parts, partition{child, v.TotalItems})
			continue
		}
		sub, err := c.plan(ctx, child, depth+1)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sub...)
	}
	return parts, nil
}

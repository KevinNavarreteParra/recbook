package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// SaveCatalog validates a batch of one library's titles and upserts them,
// then records an availability snapshot for each. seenAt is the start of
// the crawl that found them: a title keeps its first_seen_at forever and
// gets last_seen_at = seenAt, which is how dropped titles are spotted.
func (s *Store) SaveCatalog(ctx context.Context, library string, titles []contract.CatalogTitle,
	avail []contract.Availability, seenAt time.Time) error {
	if err := contract.ValidateCatalog(titles, avail); err != nil {
		return err
	}
	fetchedAt := time.Now().UTC()

	titleRows := func(add func(...any) error) error {
		for _, t := range titles {
			err := add(library, t.ID, t.Title, nullString(t.Subtitle), creatorList(t.Creators), isbn13s(t.ISBNs),
				string(t.Format), nullString(t.Edition), nullString(t.Publisher), day(t.PublishedOn),
				nullString(t.Language), nullString(t.Description), subjectList(t.Subjects),
				nullString(t.Series), nullString(t.SeriesIndex), nullInt(t.DurationMin),
				refMap(t.Refs), nullString(t.CoverURL), nullString(t.URL), seenAt, seenAt)
			if err != nil {
				return err
			}
		}
		return nil
	}
	stages := []stage{{"catalog_title", titleRows}, {"availability", availabilityRows(library, avail, fetchedAt)}}
	return s.writeStaged(ctx, stages, func(tx *sql.Tx) error {
		return execAll(ctx, tx,
			// Upsert, keeping first_seen_at from the existing row when there is one.
			`INSERT OR REPLACE INTO catalog_title
			 SELECT new.* REPLACE (coalesce(old.first_seen_at, new.first_seen_at) AS first_seen_at)
			 FROM temp.stage_catalog_title new
			 LEFT JOIN catalog_title old USING (library, title_id)`,
			`INSERT INTO availability FROM temp.stage_availability`,
		)
	})
}

// SaveSeen records that a crawl starting at seenAt listed titles already
// in the catalog: each gets last_seen_at = seenAt (as SaveCatalog would
// give it) and an availability snapshot, and keeps everything else. Titles
// not in the catalog are ignored.
func (s *Store) SaveSeen(ctx context.Context, library string, avail []contract.Availability, seenAt time.Time) error {
	return s.saveAvailability(ctx, library, avail,
		`UPDATE catalog_title SET last_seen_at = ?
		 FROM temp.stage_availability a
		 WHERE catalog_title.library = a.library AND catalog_title.title_id = a.title_id`, seenAt)
}

// SaveAvailability records an availability snapshot for each title, taken
// outside a crawl, so last_seen_at stays as the last crawl left it. Titles
// not in the catalog are ignored.
func (s *Store) SaveAvailability(ctx context.Context, library string, avail []contract.Availability) error {
	return s.saveAvailability(ctx, library, avail, "")
}

// saveAvailability stages avail, runs update (if any) with args, and adds
// the snapshots for titles in the catalog.
func (s *Store) saveAvailability(ctx context.Context, library string, avail []contract.Availability, update string, args ...any) error {
	if err := contract.ValidateAvailability(avail); err != nil {
		return err
	}
	stages := []stage{{"availability", availabilityRows(library, avail, time.Now().UTC())}}
	return s.writeStaged(ctx, stages, func(tx *sql.Tx) error {
		if update != "" {
			if _, err := tx.ExecContext(ctx, update, args...); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO availability
			 SELECT a.* FROM temp.stage_availability a
			 SEMI JOIN catalog_title t USING (library, title_id)`)
		return err
	})
}

// KnownTitles returns the ids of library's titles already in the catalog.
func (s *Store) KnownTitles(ctx context.Context, library string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT title_id FROM catalog_title WHERE library = ?`, library)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

// availabilityRows feeds avail to a stage of the availability table.
func availabilityRows(library string, avail []contract.Availability, fetchedAt time.Time) Rows {
	return func(add func(...any) error) error {
		for _, a := range avail {
			var wait any
			if a.WaitDays != nil {
				wait = int32(*a.WaitDays)
			}
			err := add(library, a.TitleID, fetchedAt, a.Available,
				int32(a.CopiesOwned), int32(a.CopiesAvailable), int32(a.Holds), wait)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// StartCrawl records that a crawl of library began, and returns its start
// time to pass to SaveCatalog and FinishCrawl.
func (s *Store) StartCrawl(ctx context.Context, library string) (time.Time, error) {
	started := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO crawl (library, started_at) VALUES (?, ?)`, library, started)
	return started, err
}

// FinishCrawl marks the crawl that began at started as complete.
func (s *Store) FinishCrawl(ctx context.Context, library string, started time.Time, titles int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE crawl SET finished_at = ?, titles = ? WHERE library = ? AND started_at = ?`,
		time.Now().UTC(), titles, library, started)
	return err
}

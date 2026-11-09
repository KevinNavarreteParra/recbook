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
	availRows := func(add func(...any) error) error {
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

	stages := []stage{{"catalog_title", titleRows}, {"availability", availRows}}
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

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

	return s.inTx(ctx, func(tx *sql.Tx) error {
		upsert, err := tx.PrepareContext(ctx, `INSERT INTO catalog_title (
			library, title_id, title, subtitle, creators, isbns, format, edition, publisher,
			published_on, language, description, subjects, series, series_index, duration_min,
			refs, cover_url, url, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (library, title_id) DO UPDATE SET
			title = excluded.title, subtitle = excluded.subtitle, creators = excluded.creators,
			isbns = excluded.isbns, format = excluded.format, edition = excluded.edition,
			publisher = excluded.publisher, published_on = excluded.published_on,
			language = excluded.language, description = excluded.description,
			subjects = excluded.subjects, series = excluded.series,
			series_index = excluded.series_index, duration_min = excluded.duration_min,
			refs = excluded.refs, cover_url = excluded.cover_url, url = excluded.url,
			last_seen_at = excluded.last_seen_at`)
		if err != nil {
			return err
		}
		defer upsert.Close()
		for _, t := range titles {
			_, err := upsert.ExecContext(ctx,
				library, t.ID, t.Title, nullString(t.Subtitle), creatorList(t.Creators), isbn13s(t.ISBNs),
				string(t.Format), nullString(t.Edition), nullString(t.Publisher), t.PublishedOn,
				nullString(t.Language), nullString(t.Description), subjectList(t.Subjects),
				nullString(t.Series), nullString(t.SeriesIndex), nullInt(t.DurationMin),
				refMap(t.Refs), nullString(t.CoverURL), nullString(t.URL), seenAt, seenAt)
			if err != nil {
				return err
			}
		}

		insertAvail, err := tx.PrepareContext(ctx, `INSERT INTO availability VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertAvail.Close()
		for _, a := range avail {
			_, err := insertAvail.ExecContext(ctx, library, a.TitleID, fetchedAt,
				a.Available, a.CopiesOwned, a.CopiesAvailable, a.Holds, a.WaitDays)
			if err != nil {
				return err
			}
		}
		return nil
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

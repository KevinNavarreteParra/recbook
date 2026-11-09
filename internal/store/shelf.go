package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// ReplaceShelf validates a source's shelf and swaps it in for whatever that
// source had before. Exports are snapshots, so re-ingesting a newer export
// replaces the older one instead of adding to it. file is recorded so
// `recbook status` can say where the data came from.
func (s *Store) ReplaceShelf(ctx context.Context, source, file string, shelf *contract.Shelf) error {
	if err := shelf.Validate(); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"shelf_event", "shelf_item"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE source = ?", source); err != nil {
				return err
			}
		}

		insertItem, err := tx.PrepareContext(ctx, `INSERT INTO shelf_item (
			source, item_id, title, subtitle, authors, isbns, format, refs,
			description, subjects, series, series_index, language, pub_year, pages
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertItem.Close()
		for _, it := range shelf.Items {
			_, err := insertItem.ExecContext(ctx,
				source, it.ID, it.Title, nullString(it.Subtitle), list(it.Authors), isbn13s(it.ISBNs),
				string(it.Format), refMap(it.Refs), nullString(it.Description), list(it.Subjects),
				nullString(it.Series), nullString(it.SeriesIndex), nullString(it.Language),
				nullInt(it.PubYear), nullInt(it.Pages))
			if err != nil {
				return err
			}
		}

		insertEvent, err := tx.PrepareContext(ctx, `INSERT INTO shelf_event (
			source, item_id, kind, happened_on, date_quality, detail
		) VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertEvent.Close()
		for _, ev := range shelf.Events {
			quality := ev.DateQuality
			if ev.On.IsZero() {
				quality = contract.DateMissing
			}
			_, err := insertEvent.ExecContext(ctx,
				source, ev.ItemID, string(ev.Kind), ev.On, string(quality), nullString(ev.Detail))
			if err != nil {
				return err
			}
		}

		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO shelf_import VALUES (?, ?, ?, ?, ?)`,
			source, file, time.Now().UTC(), len(shelf.Items), len(shelf.Events))
		return err
	})
}

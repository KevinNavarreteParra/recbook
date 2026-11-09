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

	items := func(add func(...any) error) error {
		for _, it := range shelf.Items {
			err := add(source, it.ID, it.Title, nullString(it.Subtitle), list(it.Authors), isbn13s(it.ISBNs),
				string(it.Format), refMap(it.Refs), nullString(it.Description), list(it.Subjects),
				nullString(it.Series), nullString(it.SeriesIndex), nullString(it.Language),
				nullInt(it.PubYear), nullInt(it.Pages))
			if err != nil {
				return err
			}
		}
		return nil
	}
	events := func(add func(...any) error) error {
		for _, ev := range shelf.Events {
			quality := ev.DateQuality
			if ev.On.IsZero() {
				quality = contract.DateMissing
			}
			if err := add(source, ev.ItemID, string(ev.Kind), day(ev.On), string(quality), nullString(ev.Detail)); err != nil {
				return err
			}
		}
		return nil
	}

	return s.writeStaged(ctx, []stage{{"shelf_item", items}, {"shelf_event", events}}, func(tx *sql.Tx) error {
		for _, table := range []string{"shelf_event", "shelf_item"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE source = ?", source); err != nil {
				return err
			}
		}
		if err := execAll(ctx, tx,
			`INSERT INTO shelf_item FROM temp.stage_shelf_item`,
			`INSERT INTO shelf_event FROM temp.stage_shelf_event`,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO shelf_import VALUES (?, ?, ?, ?, ?)`,
			source, file, time.Now().UTC(), len(shelf.Items), len(shelf.Events))
		return err
	})
}

// Package contract defines the canonical records every data source must
// produce. It is the one place to read to learn what recbook needs.
//
// recbook never reads an export file or an API response directly. Each
// source (ReadingList, Libby, LibraryThing, OverDrive, or a new one such as
// Goodreads) has an adapter in internal/sources that translates its data
// into these records, and everything downstream (matching, labels, tiers,
// recommendations) works only from them. Adding a source means writing one
// adapter that passes Validate; nothing else changes.
//
// The contract has two halves:
//
//   - The shelf (shelf.go): my history with books, as items and events.
//   - The catalog (catalog.go): what a library can lend, as titles and
//     availability snapshots.
//
// Adapters report facts, not judgments. A source says "finished on
// 2023-04-02, a date I typed in"; it never decides whether that makes the
// book a strong seed. Judgments such as batch-entered dates, signal tiers
// and labels are derived later, the same way for every source.
package contract

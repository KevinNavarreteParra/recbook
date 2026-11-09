package contract

// Shelf is everything one source knows about my history with books.
// Exports are snapshots: a newer Shelf from the same source replaces the
// older one entirely.
type Shelf struct {
	Items  []ShelfItem
	Events []ShelfEvent
}

// ShelfItem is one book as a source records it: usually a particular
// edition, sometimes the work in general. A source never has to know about
// the other sources. Matching items across sources, and to the catalog,
// happens later.
type ShelfItem struct {
	// ID is the source's own identifier for the record. It must stay the same
	// across exports, because events and match overrides refer to it.
	ID string `json:"id"`

	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`

	// Authors lists the primary author first, as "First Last" or
	// "Last, First". Translators and narrators don't belong here.
	Authors []string `json:"authors"`

	// ISBNs holds every ISBN the source ties to this item, as ISBN-10 or
	// ISBN-13, with or without hyphens. They are stored as ISBN-13.
	ISBNs []string `json:"isbns,omitempty"`

	Format Format `json:"format"`

	// Refs maps an external system (one of the Ref constants) to this
	// book's id there. Two records that share a ref are the same work.
	Refs map[string]string `json:"refs,omitempty"`

	// The rest is optional description. It feeds similarity when the
	// library doesn't carry the book.
	Description string   `json:"description,omitempty"` // blurb or summary as given; HTML allowed
	Subjects    []string `json:"subjects,omitempty"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex string   `json:"series_index,omitempty"` // position as written: "3", "2.5"
	Language    string   `json:"language,omitempty"`     // ISO 639-1 where known: "en"
	PubYear     int      `json:"pub_year,omitempty"`
	Pages       int      `json:"pages,omitempty"`
}

// ShelfEvent is one thing that happened between me and an item.
type ShelfEvent struct {
	ItemID string    `json:"item_id"` // the ShelfItem.ID it happened to
	Kind   EventKind `json:"kind"`

	// On is when it happened. Leave it zero when the source doesn't say.
	On Day `json:"on,omitzero"`

	// DateQuality says how far to trust On. It is required whenever On is
	// set, and ignored (stored as DateMissing) when it isn't.
	DateQuality DateQuality `json:"date_quality,omitempty"`

	// Detail is only used by Returned: ReturnedEarly or ReturnedExpired,
	// or empty when the source can't tell.
	Detail string `json:"detail,omitempty"`
}

// EventKind is the vocabulary of things that can happen to a book.
type EventKind string

const (
	Wanted    EventKind = "wanted"    // put on a to-read list
	Started   EventKind = "started"   // began reading or listening
	Finished  EventKind = "finished"  // read or listened to the end
	Abandoned EventKind = "abandoned" // the source records that I gave up on it (a "did not finish" shelf)
	Acquired  EventKind = "acquired"  // got my own copy

	// Library loans. Libby records these itself, so their dates are reliable.
	HoldPlaced EventKind = "hold_placed"
	Borrowed   EventKind = "borrowed"
	Renewed    EventKind = "renewed"
	Returned   EventKind = "returned" // see ShelfEvent.Detail
)

// EventKinds lists every valid EventKind.
var EventKinds = []EventKind{Wanted, Started, Finished, Abandoned, Acquired, HoldPlaced, Borrowed, Renewed, Returned}

// Values for ShelfEvent.Detail on a Returned event.
const (
	ReturnedEarly   = "early"   // I returned it before the loan ran out
	ReturnedExpired = "expired" // the loan ran out
)

// DateQuality says where a date came from, and so how far to trust it.
type DateQuality string

const (
	// DateSystem is a date a system recorded by itself, such as a Libby
	// borrow. It is reliable.
	DateSystem DateQuality = "system"

	// DateUser is a date a person typed in, such as a finish date logged in
	// a reading app. It is fuzzy: often a month off, sometimes entered in a
	// catch-up batch. (Batches are detected later and get their own
	// quality, user_batch; sources never report it.)
	DateUser DateQuality = "user"

	// DateMissing means there is no date.
	DateMissing DateQuality = "missing"
)

// Format is the physical form of a book.
type Format string

const (
	Print         Format = "print"
	Ebook         Format = "ebook"
	Audiobook     Format = "audiobook"
	UnknownFormat Format = "unknown"
)

// Formats lists every valid Format.
var Formats = []Format{Print, Ebook, Audiobook, UnknownFormat}

// Keys for ShelfItem.Refs and CatalogTitle.Refs.
const (
	RefOverDrive        = "overdrive"         // OverDrive title id, as in Libby URLs
	RefLibraryThingWork = "librarything_work" // LibraryThing workcode (groups editions)
	RefGoodreads        = "goodreads"         // Goodreads book id
	RefOpenLibraryWork  = "openlibrary_work"  // Open Library work key, "OL45883W"
	RefGoogleBooks      = "google_books"      // Google Books volume id
)

// RefKeys lists every valid ref key.
var RefKeys = []string{RefOverDrive, RefLibraryThingWork, RefGoodreads, RefOpenLibraryWork, RefGoogleBooks}

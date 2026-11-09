package contract

// CatalogTitle is one borrowable title in a library's collection: the search
// universe that recommendations come from.
type CatalogTitle struct {
	// ID is the catalog's own identifier (for OverDrive, the title id in
	// Libby URLs). It is unique within one library.
	ID string

	Title    string
	Subtitle string
	Creators []Creator // authors first, in the order the catalog lists them
	ISBNs    []string  // as for ShelfItem.ISBNs
	Format   Format
	Edition  string // "Unabridged", "Abridged", ...

	Publisher   string
	PublishedOn Day
	Language    string // ISO 639-1: "en"

	// Description is the publisher blurb exactly as the catalog gives it,
	// HTML and all. Cleaning happens when it's embedded, so the raw text is
	// never lost.
	Description string

	Subjects    []Subject
	Series      string
	SeriesIndex string
	DurationMin int // running time in minutes; 0 if unknown

	Refs     map[string]string // as for ShelfItem.Refs
	CoverURL string
	URL      string // where to borrow it
}

// Creator is a person credited on a title.
type Creator struct {
	Name string
	Role Role
}

// Role is what a Creator did.
type Role string

const (
	Author     Role = "author"
	Narrator   Role = "narrator"
	Translator Role = "translator"
	Editor     Role = "editor"
	OtherRole  Role = "other"
)

// Roles lists every valid Role.
var Roles = []Role{Author, Narrator, Translator, Editor, OtherRole}

// Subject is a category assigned to a title by some classification scheme.
type Subject struct {
	Scheme string // who assigned it: "overdrive", "bisac", ...
	Code   string // the scheme's id for it, if it has one: "FIC031000"
	Name   string // "Fiction / Thrillers / General"
}

// Availability is how borrowable a title is at the moment it was fetched.
type Availability struct {
	TitleID         string // the CatalogTitle.ID
	Available       bool   // can be borrowed right now
	CopiesOwned     int
	CopiesAvailable int
	Holds           int  // people waiting
	WaitDays        *int // the library's wait estimate; nil when it gives none
}

package demo

import "github.com/KevinNavarreteParra/recbook/internal/contract"

// Catalog returns the demo library's audiobooks and their availability.
func Catalog() ([]contract.CatalogTitle, []contract.Availability) {
	type entry struct {
		id, title, subtitle, author, narrator string
		available                             bool
		wait                                  int // days; 0 when available
	}
	entries := []entry{
		// On the demo shelves, matched by OverDrive id (the loans) ...
		{"9001", "The Hunt for Red October", "", "Tom Clancy", "Scott Brick", false, 21},
		{"9002", "The Da Vinci Code", "", "Dan Brown", "Paul Michael", true, 0},
		{"9003", "The Spy Who Came in from the Cold", "", "John le Carré", "Michael Jayston", true, 0},
		{"9004", "Gone Girl", "", "Gillian Flynn", "Julia Whelan", false, 60},
		{"9005", "Project Hail Mary", "", "Andy Weir", "Ray Porter", false, 90},
		// ... or by title and author (the rest).
		{"9010", "The Brothers Karamazov", "", "Fyodor Dostoevsky", "Constantine Gregory", true, 0},
		{"9014", "The Brothers Karamazov", "", "Fyodor Dostoevsky", "Simon Vance", false, 7}, // a second recording
		{"9011", "A Gentleman in Moscow", "", "Amor Towles", "Nicholas Guy Smith", false, 30},
		{"9012", "Ethics", "Demonstrated in Geometrical Order", "Benedict de Spinoza", "Kevin Theis", true, 0},
		{"9013", "Tinker Tailor Soldier Spy", "", "John le Carré", "Michael Jayston", true, 0},
		{"9015", "Meditations", "A New Translation", "Marcus Aurelius", "Duncan Steen", false, 14},
		{"9019", "The Republic", "", "Plato", "Ray Childs", false, 45},
		// Not on any shelf: what recommendations would draw from.
		{"9016", "Smiley's People", "", "John le Carré", "Michael Jayston", true, 0},
		{"9017", "Red Storm Rising", "", "Tom Clancy", "Michael Prichard", false, 10},
		{"9018", "Dune Messiah", "", "Frank Herbert", "Scott Brick", true, 0},
	}

	var titles []contract.CatalogTitle
	var avail []contract.Availability
	for _, e := range entries {
		titles = append(titles, contract.CatalogTitle{
			ID:       e.id,
			Title:    e.title,
			Subtitle: e.subtitle,
			Creators: []contract.Creator{
				{Name: e.author, Role: contract.Author},
				{Name: e.narrator, Role: contract.Narrator},
			},
			Format:      contract.Audiobook,
			Language:    "en",
			DurationMin: 600,
			Refs:        map[string]string{contract.RefOverDrive: e.id},
			URL:         "https://libbyapp.com/library/" + Library + "/everything/page-1/" + e.id,
		})
		a := contract.Availability{TitleID: e.id, Available: e.available, CopiesOwned: 2}
		if e.available {
			a.CopiesAvailable = 1
		} else {
			wait := e.wait
			a.Holds, a.WaitDays = 3, &wait
		}
		avail = append(avail, a)
	}
	return titles, avail
}

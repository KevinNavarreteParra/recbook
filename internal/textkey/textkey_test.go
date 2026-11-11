package textkey

import "testing"

func TestTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Hunt for Red October", "hunt for red october"},
		{"The Hunt for Red October (Jack Ryan, #1)", "hunt for red october"}, // Goodreads-style series note
		{"The Hunt for Red October: A Novel", "hunt for red october"},
		{"Crime and Punishment (Unabridged)", "crime and punishment"},
		{"Tinker, Tailor, Soldier, Spy", "tinker tailor soldier spy"},
		{"Tinker Tailor Soldier Spy", "tinker tailor soldier spy"},
		{"Pride & Prejudice", "pride and prejudice"},
		{"Les Misérables", "les miserables"},
		{"A Gentleman in Moscow", "gentleman in moscow"},
		{"A", "a"}, // an article alone is the whole title
		{"Ethics: Demonstrated in Geometrical Order", "ethics demonstrated in geometrical order"},
		{"Beloved: A Novel", "beloved"},
		{"Novel Ideas", "novel ideas"}, // "novel" only goes when it's "a novel" at the end
	}
	for _, tt := range tests {
		if got := Title(tt.in); got != tt.want {
			t.Errorf("Title(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMainTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ethics: Demonstrated in Geometrical Order", "ethics"},
		{"Ethics", "ethics"},
		{"Star Wars: Thrawn", "star wars"},
	}
	for _, tt := range tests {
		if got := MainTitle(tt.in); got != tt.want {
			t.Errorf("MainTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAuthor(t *testing.T) {
	tests := []struct{ in, want string }{
		{"John le Carré", "carre j"},
		{"le Carré, John", "carre j"},
		{"J.R.R. Tolkien", "tolkien j"},
		{"Tolkien, J. R. R.", "tolkien j"},
		{"Fyodor Dostoevsky", "dostoevsky f"},
		{"Gabriel García Márquez", "marquez g"},
		{"García Márquez, Gabriel", "marquez g"},
		{"Martin Luther King Jr.", "king m"},
		{"King, Martin Luther, Jr.", "king m"},
		{"Flannery O'Connor", "oconnor f"},
		{"Homer", "homer"},
		{"Ursula K. Le Guin", "guin u"},
		{"Le Guin, Ursula K.", "guin u"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Author(tt.in); got != tt.want {
			t.Errorf("Author(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsPlaceholderAuthor(t *testing.T) {
	if !IsPlaceholderAuthor(Author("Anonymous")) || !IsPlaceholderAuthor(Author("Various")) {
		t.Error("Anonymous and Various are placeholders")
	}
	if IsPlaceholderAuthor(Author("Homer")) {
		t.Error("Homer is a person")
	}
}

func TestPerson(t *testing.T) {
	tests := []struct{ in, want string }{
		{"John le Carré", "john le carre"},
		{"John Le Carre", "john le carre"},
		{"le Carré, John", "john le carre"},
		{"J.M. Barrie", "j m barrie"},
		{"J. M. Barrie", "j m barrie"},
		{"JM Barrie", "j m barrie"},
		{"Barrie, J. M.", "j m barrie"},
		{"James Matthew Barrie", "james matthew barrie"}, // not merged: see Person
		{"Rousseau, Jean-Jacques", "jean jacques rousseau"},
		{"L. E. Modesitt, Jr.", "l e modesitt"},
		{"Martin Luther King Jr.", "martin luther king"},
		{"King, Martin Luther, Jr.", "martin luther king"},
		{"Deepak Chopra, M.D.", "deepak chopra"},
		{"Clarissa Pinkola Estés, Ph.D., PhD", "clarissa pinkola estes"},
		{"Plato, Plato", "plato"},
		{"Celeste Ng", "celeste ng"}, // a short surname is not initials
		{"Ng, Celeste", "celeste ng"},
		{"Flannery O'Connor", "flannery oconnor"},
		{"Anonymous", ""},
		{"Various Authors", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Person(tt.in); got != tt.want {
			t.Errorf("Person(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLikelySamePerson(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"fyodor dostoyevsky", "fyodor dostoevsky", true},
		{"hermann hesse", "herman hesse", true},
		{"edgar allen poe", "edgar allan poe", true},
		{"leo tolstoy", "leo tolstoi", true},
		{"john smith", "jane smith", false},              // short words: too easily two people
		{"jesse kellerman", "jonathan kellerman", false}, // too far apart
		{"min jin lee", "m j lee", false},
		{"j m barrie", "james matthew barrie", false},
		{"john le carre", "john le carre", false}, // the same key is the same author, not a spelling
		{"plato", "aristotle", false},
	}
	for _, tt := range tests {
		if got := LikelySamePerson(tt.a, tt.b); got != tt.want {
			t.Errorf("LikelySamePerson(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "", 3}, {"kitten", "sitting", 3}, {"dostoyevsky", "dostoevsky", 1},
	}
	for _, tt := range tests {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

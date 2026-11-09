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

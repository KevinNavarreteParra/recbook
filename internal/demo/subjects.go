package demo

import "github.com/KevinNavarreteParra/recbook/internal/contract"

// Subjects are BISAC headings for the catalog's titles, by title id, in
// the shape OverDrive gives them. They shelve the three le Carré novels
// under Espionage and the two Clancys under Military, and the rest apart.
// Tests that need the genre view copy them onto Catalog's titles.
var Subjects = map[string][]contract.Subject{
	"9001": bisac("Fiction / Thrillers / Military", "Fiction / Action & Adventure"),
	"9002": bisac("Fiction / Thrillers / Suspense", "Fiction / Mystery & Detective / General"),
	"9003": bisac("Fiction / Thrillers / Espionage", "Fiction / Literary"),
	"9004": bisac("Fiction / Thrillers / Psychological", "Fiction / Thrillers / Suspense"),
	"9005": bisac("Fiction / Science Fiction / Hard Science Fiction"),
	"9010": bisac("Fiction / Classics", "Fiction / Literary"),
	"9014": bisac("Fiction / Classics", "Fiction / Literary"),
	"9022": bisac("Fiction / Classics", "Fiction / Literary"),
	"9011": bisac("Fiction / Historical / General", "Fiction / Literary"),
	"9021": bisac("Fiction / Historical / General", "Fiction / Literary"),
	"9012": bisac("Philosophy / Ethics & Moral Philosophy"),
	"9013": bisac("Fiction / Thrillers / Espionage"),
	"9015": bisac("Philosophy / Ethics & Moral Philosophy", "Philosophy / History & Surveys / Ancient & Classical"),
	"9019": bisac("Philosophy / History & Surveys / Ancient & Classical", "Political Science / History & Theory"),
	"9016": bisac("Fiction / Thrillers / Espionage"),
	"9017": bisac("Fiction / Thrillers / Military", "Fiction / War & Military"),
	"9018": bisac("Fiction / Science Fiction / Space Opera"),
}

func bisac(names ...string) []contract.Subject {
	var out []contract.Subject
	for _, n := range names {
		out = append(out, contract.Subject{Scheme: "bisac", Name: n})
	}
	return out
}

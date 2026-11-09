package blurb

import "testing"

func TestClean(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			"html and entities",
			"<p>A keeper finds a ledger&#8212;and a secret.</p><p>The tide&nbsp;turns.</p>",
			"A keeper finds a ledger—and a secret. The tide turns.",
		},
		{
			"praise paragraphs and bestseller lines",
			"<p>“Luminous and unforgettable.” —The Invented Review</p>" +
				"<p>From the #1 New York Times bestselling author of The Tides comes a new novel. " +
				"A keeper finds a ledger that names every ship the light failed.</p>",
			"A keeper finds a ledger that names every ship the light failed.",
		},
		{
			"awards and book clubs",
			"Winner of the Invented Prize for Fiction. An instant Reese's Book Club pick. " +
				"Two sisters inherit a lighthouse.",
			"Two sisters inherit a lighthouse.",
		},
		{
			"audiobook boilerplate and series marketing",
			"Two sisters inherit a lighthouse. Narrated by June Okafor. Includes a bonus PDF of maps. " +
				"Don't miss the next thrilling installment!",
			"Two sisters inherit a lighthouse.",
		},
		{
			"inline quoted praise",
			`Two sisters inherit a lighthouse. "Gripping" —Invented Weekly.`,
			"Two sisters inherit a lighthouse.",
		},
		{
			"series marketing only at the start of a sentence",
			"She goes to look for the book her mother hid. Look for the next Northern Coast novel.",
			"She goes to look for the book her mother hid.",
		},
		{
			"plain description is untouched",
			"Two sisters inherit a lighthouse on a coast that eats ships. One of them wants to sell.",
			"Two sisters inherit a lighthouse on a coast that eats ships. One of them wants to sell.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestIsThin(t *testing.T) {
	if !IsThin("A short blurb.") {
		t.Error("a one-line blurb is thin")
	}
	long := "Two sisters inherit a lighthouse on a coast that eats ships. One of them wants to sell it; " +
		"the other has found a ledger that says the light was put out on purpose."
	if IsThin(long) {
		t.Error("two full sentences are not thin")
	}
}

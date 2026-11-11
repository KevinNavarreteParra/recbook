package demo

import (
	"context"
	"math"
	"strings"
)

// Blurbs are descriptions for some of the catalog's titles, by title id,
// written so that ThemeEmbedder places them predictably: three le Carré
// spy novels together, two navy thrillers together, and the rest apart.
// Tests that need the content view copy them onto Catalog's titles.
var Blurbs = map[string]string{
	"9013": "A mole inside the Circus. Smiley, a retired spy, hunts him through his old Berlin agents.",
	"9016": "Smiley hunts his old adversary across Europe, calling in agents from Berlin. A spy's last case.",
	"9003": "A burned-out spy in Berlin takes one last mission for the Circus.",
	"9018": "The emperor of the desert finds his own prophecies closing around him across the galaxy.",
	"9017": "A submarine war in the North Atlantic, fought by the navy.",
	"9001": "A Soviet submarine captain defects, and the navy races to find him before war breaks out.",
	"9010": "Three brothers in provincial Russia, a murdered father, and a long argument about faith.",
}

// ThemeEmbedder is a stand-in for the embedding model (an embed.Embedder)
// that makes "similar" predictable: each vector has one dimension per
// theme, counting the theme's words in the text, plus a constant.
type ThemeEmbedder struct{}

var themes = [][]string{
	{"spy", "smiley", "circus", "berlin", "agent"},
	{"desert", "emperor", "prophec", "galaxy"},
	{"submarine", "navy", "war"},
	{"brothers", "faith", "russia"},
}

// Embed returns a unit-length theme vector for each input.
func (ThemeEmbedder) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	var out [][]float32
	for _, in := range inputs {
		in = strings.ToLower(in)
		v := []float32{0.1}
		for _, words := range themes {
			var n float32
			for _, w := range words {
				n += float32(strings.Count(in, w))
			}
			v = append(v, n)
		}
		var sum float64
		for _, x := range v {
			sum += float64(x * x)
		}
		for i := range v {
			v[i] /= float32(math.Sqrt(sum))
		}
		out = append(out, v)
	}
	return out, nil
}

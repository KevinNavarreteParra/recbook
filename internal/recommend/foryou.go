package recommend

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// For you: recommendations from my whole history at once, with every book
// I've finished as a seed (#3).
//
// Fusing that many seeds' rankings by reciprocal rank, as Similar does for
// a few, rewards breadth: a book fairly near fifty seeds outscores the
// nearest book of all to five. With four hundred seeds, the top fills with
// surveys (a history of philosophy, a lecture series) that every seed lists
// somewhere, and forty-odd philosophy seeds, shelved alike, vote as one
// block. So ForYou first puts the seeds into groups that read alike, fuses
// each group alone, where agreement among seeds is what's wanted, and lets
// the groups take turns at the list in proportion to their seeds' weight.

var (
	//go:embed history_seeds.sql
	historySeedsSQL string
	//go:embed for_you.sql
	forYouSQL string
	//go:embed for_you_list.sql
	forYouListSQL string
)

// ForYouDepth is how long the stored list is (BuildForYou): deep enough
// that a page is left after keeping only what's available now, say.
const ForYouDepth = 500

// GroupThreshold is how alike a seed must be to a group's leader to join
// the group: the cosine of their centred content vectors (see GroupSeeds).
// Of 0.3 to 0.8, 0.5 ranked held-out books best in the evaluation's history
// protocol: 6.7% in the top 50, where fusing every seed at once puts 0.4%
// there. At 0.5, 422 finished books make about 300 groups, the largest of
// ten. Fusing the groups' lists by reciprocal rank, rather than giving
// each group its share of places, did no better than fusing every seed.
const GroupThreshold = 0.5

// A Group is seeds that read alike.
type Group struct {
	Seeds  []Seed  // its leader first, then the rest, heaviest first
	Weight float64 // the sum of its seeds' weights
}

// Leader is the seed the group formed around, which it goes by.
func (g Group) Leader() Seed { return g.Seeds[0] }

// GroupSeeds puts seeds into groups by leader clustering. Seeds are taken
// heaviest first, and each joins the group whose leader it's most like, if
// at least threshold alike, or else leads a new group. Likeness is the
// cosine of the two content vectors less mean, the catalog's mean vector:
// any two blurbs' raw vectors are close (most cosines fall in 0.6 to 0.9),
// and centring spreads them out. A seed with no content vector leads a
// group of its own. Groups come out heaviest first.
func GroupSeeds(seeds []Seed, mean []float32, threshold float64) []Group {
	order := slices.Clone(seeds)
	slices.SortStableFunc(order, func(a, b Seed) int {
		if c := cmp.Compare(b.Weight, a.Weight); c != 0 {
			return c
		}
		return cmp.Compare(a.WorkID, b.WorkID)
	})

	var groups []Group
	var leaders [][]float64 // each group's leader, centred; nil if it has no vector
	for _, s := range order {
		v := centred(s.vector, mean)
		best, bestSim := -1, threshold
		for i, l := range leaders {
			if v == nil || l == nil {
				continue
			}
			if sim := dot(v, l); sim >= bestSim {
				best, bestSim = i, sim
			}
		}
		if best < 0 {
			groups = append(groups, Group{})
			leaders = append(leaders, v)
			best = len(groups) - 1
		}
		groups[best].Seeds = append(groups[best].Seeds, s)
		groups[best].Weight += s.Weight
	}
	slices.SortStableFunc(groups, func(a, b Group) int { return cmp.Compare(b.Weight, a.Weight) })
	return groups
}

// centred returns v less mean, scaled to unit length, or nil for no vector.
func centred(v, mean []float32) []float64 {
	if v == nil || len(v) != len(mean) {
		return nil
	}
	out := make([]float64, len(v))
	var norm float64
	for i := range v {
		out[i] = float64(v[i]) - float64(mean[i])
		norm += out[i] * out[i]
	}
	norm = math.Sqrt(norm)
	for i := range out {
		out[i] /= norm
	}
	return out
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Interleave merges the groups' lists into one, best first. Each place
// goes to the group with the highest weight / (2·places so far + 1), the
// Sainte-Laguë rule, which hands out places in proportion to weight while
// letting a smaller group in before the biggest has taken everything. A
// group takes its best book not yet listed, keeping to opts' per-author
// cap; groups[i] and lists[i] belong together.
func Interleave(groups []Group, lists [][]Rec, opts Options) []Rec {
	if opts.Limit == 0 {
		opts.Limit = 20
	}
	next := make([]int, len(lists))   // how far down each list has been read
	places := make([]int, len(lists)) // how many places each group has
	taken := map[int64]bool{}
	byAuthor := map[string]int{}
	var out []Rec
	for len(out) < opts.Limit {
		best, bestQuota := -1, 0.0
		for i := range lists {
			if next[i] >= len(lists[i]) {
				continue // nothing left
			}
			if q := groups[i].Weight / float64(2*places[i]+1); best < 0 || q > bestQuota {
				best, bestQuota = i, q
			}
		}
		if best < 0 {
			break // every list is used up
		}
		for next[best] < len(lists[best]) {
			r := lists[best][next[best]]
			next[best]++
			author := authorKey(r.Author)
			if taken[r.WorkID] || (opts.PerAuthor > 0 && byAuthor[author] >= opts.PerAuthor) {
				continue
			}
			taken[r.WorkID] = true
			byAuthor[author]++
			r.Group = best
			out = append(out, r)
			places[best]++
			break
		}
	}
	return out
}

// FuseGroups fuses each group's rankings alone (by Fuse, with the avoid
// rankings pushing down as in Similar) and interleaves the groups' lists.
// The rankings are every seed's; each group takes its own seeds'.
func FuseGroups(groups []Group, pos, avoid []Ranking, opts Options) []Rec {
	of := map[int64]int{} // seed work id → its group
	for i, g := range groups {
		for _, s := range g.Seeds {
			of[s.WorkID] = i
		}
	}
	byGroup := make([][]Ranking, len(groups))
	for _, r := range pos {
		if i, ok := of[r.Seed.WorkID]; ok {
			byGroup[i] = append(byGroup[i], r)
		}
	}
	// Each group's list is cut to what it could use if it took every place,
	// with room for what the per-author cap and the other groups skip.
	groupOpts := opts
	groupOpts.PerAuthor = 0
	groupOpts.Limit = 2 * cmp.Or(opts.Limit, 20)
	lists := make([][]Rec, len(groups))
	for i := range groups {
		lists[i] = Fuse(byGroup[i], avoid, groupOpts)
	}
	return Interleave(groups, lists, opts)
}

// History is the seeds of a "for you" list: every book I've finished
// that can be compared in some view, and the books I gave up on, to push
// their likes down.
type History struct {
	Seeds  []Seed
	Avoid  []Seed
	Groups []Group
}

var errNoHistory = errors.New("no finished book has a description or subjects to compare (have build and embed run?)")

// LoadHistory loads my finished books as seeds, grouped by taste, and the
// books I gave up on, in the views of opts' blend.
func LoadHistory(ctx context.Context, st *store.Store, opts Options) (*History, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	rows, err := st.DB().QueryContext(ctx, historySeedsSQL)
	if err != nil {
		return nil, err
	}
	var finished, gaveUp []int64
	for rows.Next() {
		var id int64
		var isFinished bool
		if err := rows.Scan(&id, &isFinished); err != nil {
			rows.Close()
			return nil, err
		}
		if isFinished {
			finished = append(finished, id)
		} else {
			gaveUp = append(gaveUp, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	h := &History{}
	for _, list := range []struct {
		ids  []int64
		into *[]Seed
	}{{finished, &h.Seeds}, {gaveUp, &h.Avoid}} {
		seeds, err := LoadSeeds(ctx, st, opts.Model, list.ids)
		if err != nil {
			return nil, err
		}
		for _, s := range seeds {
			if len(opts.blend().shares(s)) > 0 {
				*list.into = append(*list.into, s)
			}
		}
	}
	if len(h.Seeds) == 0 {
		return nil, errNoHistory
	}
	mean, err := LoadMean(ctx, st, opts.Model)
	if err != nil {
		return nil, err
	}
	h.Groups = GroupSeeds(h.Seeds, mean, GroupThreshold)
	return h, nil
}

// LoadMean returns the catalog's mean content vector in model.
func LoadMean(ctx context.Context, st *store.Store, model string) ([]float32, error) {
	var mean any
	err := st.DB().QueryRowContext(ctx, `SELECT vector FROM content_mean WHERE model = ?`, model).Scan(&mean)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no mean vector for %s (has `recbook embed` run?)", model)
	}
	if err != nil {
		return nil, err
	}
	return toFloats(mean), nil
}

// ForYou recommends from my whole history: every finished book a seed,
// grouped by taste, each group fused alone and the groups interleaved.
// opts.Seeds and opts.Avoid are ignored.
func ForYou(ctx context.Context, st *store.Store, opts Options) ([]Rec, *History, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	h, err := LoadHistory(ctx, st, opts)
	if err != nil {
		return nil, nil, err
	}
	var exclude []int64
	for _, s := range h.Avoid {
		exclude = append(exclude, s.WorkID)
	}
	pos, err := Rank(ctx, st, h.Seeds, exclude, opts, PerSeed)
	if err != nil {
		return nil, nil, err
	}
	avoidOpts := opts
	avoidOpts.Unweighted = true
	neg, err := Rank(ctx, st, h.Avoid, exclude, avoidOpts, PerSeed)
	if err != nil {
		return nil, nil, err
	}
	return FuseGroups(h.Groups, pos, neg, opts), h, nil
}

// BuildForYou makes the "for you" list of recordings in languages (nil
// for any), ForYouDepth long and unfiltered otherwise, and stores it with
// its groups (for_you.sql), for ListForYou to read. Making it ranks the
// catalog for every finished book, which takes about a minute, so build
// runs it, and embed when it has added vectors. With nothing to compare
// yet, the list is empty.
func BuildForYou(ctx context.Context, st *store.Store, languages []string) error {
	recs, h, err := ForYou(ctx, st, Options{Languages: languages, MaxWaitDays: -1, PerAuthor: 3, Limit: ForYouDepth})
	if errors.Is(err, errNoHistory) {
		recs, h, err = nil, &History{}, nil
	}
	if err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	if err := st.RunScript(ctx, forYouSQL, nil); err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	err = st.AppendRows(ctx, "for_you", func(add func(...any) error) error {
		for i, r := range recs {
			if err := add(i+1, r.WorkID, r.Like, r.Similarity, r.View.String(), r.Group+1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	return st.AppendRows(ctx, "for_you_group", func(add func(...any) error) error {
		for i, g := range h.Groups {
			l := g.Leader()
			if err := add(i+1, l.WorkID, l.Title, l.Author, len(g.Seeds), g.Weight); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListOptions filter the stored "for you" list.
type ListOptions struct {
	AvailableNow bool     // only what can be borrowed right now
	MaxWaitDays  int      // only what's available within this many days; <0 for no limit
	MinMinutes   int      // only recordings at least this long; 0 for no bound
	MaxMinutes   int      // only recordings shorter than this; 0 for no bound
	Languages    []string // only recordings in these languages; nil for any
	Limit        int      // at most this many; 0 means 30
}

// GroupSummary is one group of seeds behind the stored list.
type GroupSummary struct {
	Title, Author string // its leader's
	Seeds         int
	Weight        float64
}

// ListForYou reads the stored "for you" list, in its order, keeping what
// opts allow, and the groups of seeds behind it, heaviest first.
func ListForYou(ctx context.Context, st *store.Store, opts ListOptions) ([]Rec, []GroupSummary, error) {
	languages := opts.Languages
	if languages == nil {
		languages = []string{} // a NULL list would keep nothing
	}
	rows, err := st.DB().QueryContext(ctx, forYouListSQL,
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("min_minutes", opts.MinMinutes),
		sql.Named("max_minutes", opts.MaxMinutes),
		sql.Named("languages", languages),
		sql.Named("limit", cmp.Or(opts.Limit, 30)))
	if err != nil {
		return nil, nil, fmt.Errorf("for you (has `recbook build` run?): %w", err)
	}
	defer rows.Close()
	var recs []Rec
	for rows.Next() {
		var r Rec
		var author, narrator, url, tier sql.NullString
		var view string
		err := rows.Scan(&r.WorkID, &r.TitleID, &r.Title, &author, &narrator, &r.DurationMin, &r.Abridged,
			&r.Available, &r.WaitDays, &r.Holds, &url, &tier, &r.Like, &r.Similarity, &view)
		if err != nil {
			return nil, nil, err
		}
		r.Author, r.Narrator, r.URL, r.Tier = author.String, narrator.String, url.String, tier.String
		if view == Genre.String() {
			r.View = Genre
		}
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	groups, err := st.DB().QueryContext(ctx, `SELECT title, author, seeds, weight FROM for_you_group ORDER BY rank`)
	if err != nil {
		return nil, nil, err
	}
	defer groups.Close()
	var out []GroupSummary
	for groups.Next() {
		var g GroupSummary
		if err := groups.Scan(&g.Title, &g.Author, &g.Seeds, &g.Weight); err != nil {
			return nil, nil, err
		}
		out = append(out, g)
	}
	return recs, out, groups.Err()
}

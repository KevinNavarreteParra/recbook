package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/KevinNavarreteParra/recbook/internal/store"
	"github.com/KevinNavarreteParra/recbook/internal/suite"
)

func newSuiteCmd(g *globals) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "suite",
		Short: "Rerun the saved queries, check what they return, and say what changed",
		Long: "Run each query saved in the suite file (default <data dir>/queries.txt),\n" +
			"check its results against what the file expects of them, and list the\n" +
			"books that entered or left its results since the last run, which is kept\n" +
			"in <data dir>/review/suite.txt. Run it before and after a change to the\n" +
			"ranking. A query is a similar, foryou or author command line; an author query's\n" +
			"results are its similar authors' books. The file's format is described in\n" +
			"internal/suite. Exits non-zero if any check fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				file = filepath.Join(g.dataDir(), "queries.txt")
			}
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			queries, err := suite.Parse(f)
			f.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}

			snapshotFile := filepath.Join(g.dataDir(), "review", "suite.txt")
			last, err := readSnapshot(snapshotFile)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			out := cmd.OutOrStdout()
			now := suite.Snapshot{}
			var names []string
			checks, failed := 0, 0
			for _, q := range queries {
				books, err := runSaved(ctx, st, q, g.languages())
				if err != nil {
					return fmt.Errorf("%s, line %d (%s): %w", file, q.Line, q.Name, err)
				}
				if err := suite.LoadSubjects(ctx, st, books); err != nil {
					return err
				}
				lines := make([]string, len(books))
				for i, b := range books {
					lines[i] = suite.Line(b)
				}
				now[q.Name] = lines
				names = append(names, q.Name)
				checks += len(q.Checks)
				failed += report(out, q, books, last, lines)
			}

			if err := writeSnapshot(snapshotFile, now, names); err != nil {
				return err
			}
			fmt.Fprintf(out, "%d queries, %d of %d checks pass; results saved in %s\n",
				len(queries), checks-failed, checks, snapshotFile)
			if failed > 0 {
				return fmt.Errorf("%d checks failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "the suite file (default <data dir>/queries.txt)")
	return cmd
}

// runSaved runs one saved query through the same flags and code as the
// command it names, keeping as many results as its checks look at.
func runSaved(ctx context.Context, st *store.Store, q suite.Query, languages []string) ([]suite.Book, error) {
	flags := pflag.NewFlagSet(q.Args[0], pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var books []suite.Book
	switch q.Args[0] {
	case "similar":
		var sq similarQuery
		sq.flags(flags)
		if err := flags.Parse(q.Args[1:]); err != nil {
			return nil, err
		}
		sq.opts.Limit = max(sq.opts.Limit, q.Depth())
		recs, _, err := sq.run(ctx, st, languages)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			books = append(books, suite.Book{WorkID: r.WorkID, Title: r.Title, Author: r.Author})
		}
	case "author":
		var aq authorQuery
		aq.flags(flags)
		if err := flags.Parse(q.Args[1:]); err != nil {
			return nil, err
		}
		res, _, err := aq.run(ctx, st, strings.Join(flags.Args(), " "), languages)
		if err != nil {
			return nil, err
		}
		for _, n := range res.Similar {
			for _, b := range n.Books {
				books = append(books, suite.Book{WorkID: b.WorkID, Title: b.Title, Author: n.Name})
			}
		}
	case "foryou":
		var fq forYouQuery
		fq.flags(flags)
		if err := flags.Parse(q.Args[1:]); err != nil {
			return nil, err
		}
		fq.opts.Limit = max(fq.opts.Limit, q.Depth())
		recs, _, err := fq.run(ctx, st, languages)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			books = append(books, suite.Book{WorkID: r.WorkID, Title: r.Title, Author: r.Author})
		}
	default:
		return nil, fmt.Errorf("the suite runs similar, foryou and author queries, not %q", q.Args[0])
	}
	return books[:min(len(books), q.Depth())], nil
}

// report prints one query's failed checks and its changes since the last
// run, and returns how many checks failed.
func report(out io.Writer, q suite.Query, books []suite.Book, last suite.Snapshot, lines []string) int {
	failed := 0
	var failures []string
	for _, c := range q.Checks {
		if n, ok := c.Matches(books); !ok {
			failed++
			failures = append(failures, fmt.Sprintf("  FAIL %s: %d match", c.Text, n))
		}
	}
	status := fmt.Sprintf("%d of %d checks pass", len(q.Checks)-failed, len(q.Checks))
	if len(q.Checks) == 0 {
		status = "no checks"
	}
	fmt.Fprintf(out, "%s: %s\n", q.Name, status)
	for _, f := range failures {
		fmt.Fprintln(out, f)
	}

	previous, ok := last[q.Name]
	switch changes := suite.Compare(previous, lines); {
	case !ok:
		fmt.Fprintln(out, "  (first run)")
	case len(changes) == 0:
		fmt.Fprintln(out, "  same books as last run")
	default:
		for _, c := range changes {
			if c.Was == 0 {
				fmt.Fprintf(out, "  + %2d. %s\n", c.Rank, c.Line)
			} else {
				fmt.Fprintf(out, "  -     %s (was %d)\n", c.Line, c.Was)
			}
		}
	}
	fmt.Fprintln(out)
	return failed
}

// readSnapshot reads the last run's results, or none if there was no run.
func readSnapshot(file string) (suite.Snapshot, error) {
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return suite.Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return suite.ReadSnapshot(f)
}

func writeSnapshot(file string, s suite.Snapshot, names []string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	if err := suite.WriteSnapshot(f, s, names); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

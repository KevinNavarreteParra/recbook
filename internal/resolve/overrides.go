package resolve

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// override is one hand correction from the overrides file, a CSV with a
// header row and the columns ref, link_to, note:
//
//	ref,link_to,note
//	readinglist:118,catalog:lapl:5700929,the library's edition has a different title
//	librarything:77,none,an omnibus; don't merge it with the single volumes
//
// link_to is another record's ref, which forces the two into one work, or
// "none", which cuts every automatic match on ref. note is for me, not the
// code. Overrides name records rather than works because work ids are
// renumbered on every build.
type override struct {
	ref    string
	linkTo string // "" for none
	where  string // file:line, for error messages
}

func readOverrides(path string) ([]override, error) {
	if path == "" || !fileExists(path) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // the note column is optional
	r.Comment = '#'
	var out []override
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		line, _ := r.FieldPos(0)
		where := fmt.Sprintf("%s:%d", path, line)
		if len(out) == 0 && rec[0] == "ref" {
			continue // header
		}
		if len(rec) < 2 || strings.TrimSpace(rec[0]) == "" || strings.TrimSpace(rec[1]) == "" {
			return nil, fmt.Errorf("%s: want ref,link_to[,note]", where)
		}
		o := override{ref: strings.TrimSpace(rec[0]), linkTo: strings.TrimSpace(rec[1]), where: where}
		if o.linkTo == "none" {
			o.linkTo = ""
		}
		out = append(out, o)
	}
	return out, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Package nightly holds the rules that keep the nightly job out of the
// way: it runs only inside a window of the night, and it leaves the GPU
// alone whenever something else is using it.
package nightly

import (
	"fmt"
	"strings"
	"time"
)

// Window is a stretch of each day, such as 02:00 to 06:00. It may cross
// midnight (23:00-05:00).
type Window struct {
	start, end time.Duration // since midnight
}

// ParseWindow reads "HH:MM-HH:MM".
func ParseWindow(s string) (Window, error) {
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return Window{}, fmt.Errorf("window %q: want HH:MM-HH:MM", s)
	}
	start, err := clock(from)
	if err != nil {
		return Window{}, fmt.Errorf("window %q: %w", s, err)
	}
	end, err := clock(to)
	if err != nil {
		return Window{}, fmt.Errorf("window %q: %w", s, err)
	}
	if start == end {
		return Window{}, fmt.Errorf("window %q is empty", s)
	}
	return Window{start, end}, nil
}

// End reports whether now falls inside the window, and if so when the
// window closes.
func (w Window) End(now time.Time) (time.Time, bool) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since := now.Sub(midnight)

	if w.start < w.end { // same day: 02:00-06:00
		if since >= w.start && since < w.end {
			return midnight.Add(w.end), true
		}
		return time.Time{}, false
	}
	// Crossing midnight: 23:00-05:00.
	switch {
	case since >= w.start:
		return midnight.AddDate(0, 0, 1).Add(w.end), true
	case since < w.end:
		return midnight.Add(w.end), true
	}
	return time.Time{}, false
}

func (w Window) String() string {
	return fmt.Sprintf("%s-%s", hhmm(w.start), hhmm(w.end))
}

func clock(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func hhmm(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

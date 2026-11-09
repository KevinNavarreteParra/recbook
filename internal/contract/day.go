package contract

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Day is a calendar date with no time of day. The zero Day means the date is
// unknown. In JSON a Day is "2006-01-02", and an unknown Day is "" or null.
type Day struct {
	t time.Time // always midnight UTC, or the zero time
}

// NewDay returns the Day for a year, month and day of the month.
func NewDay(year int, month time.Month, day int) Day {
	return Day{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// DayOf returns the calendar date of t in t's own time zone.
func DayOf(t time.Time) Day {
	if t.IsZero() {
		return Day{}
	}
	y, m, d := t.Date()
	return NewDay(y, m, d)
}

// ParseDay parses s with layout (a time package layout such as
// time.DateOnly). An empty s is the unknown Day, not an error.
func ParseDay(layout, s string) (Day, error) {
	if s == "" {
		return Day{}, nil
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return Day{}, err
	}
	return DayOf(t), nil
}

// IsZero reports whether the date is unknown.
func (d Day) IsZero() bool { return d.t.IsZero() }

// Time returns the date as midnight UTC.
func (d Day) Time() time.Time { return d.t }

// String returns "2006-01-02", or "" for the unknown Day.
func (d Day) String() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format(time.DateOnly)
}

func (d Day) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Day) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("day must be a \"YYYY-MM-DD\" string: %w", err)
	}
	if s == nil {
		*d = Day{}
		return nil
	}
	parsed, err := ParseDay(time.DateOnly, *s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Value lets a Day be passed straight to a SQL query: unknown becomes NULL.
func (d Day) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.t, nil
}

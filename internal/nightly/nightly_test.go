package nightly

import (
	"context"
	"testing"
	"time"
)

func TestWindowEnd(t *testing.T) {
	at := func(hhmm string) time.Time {
		t, _ := time.Parse("2006-01-02 15:04", "2026-11-09 "+hhmm)
		return t
	}
	tests := []struct {
		window, now string
		inside      bool
		end         string // "2006-01-02 15:04"
	}{
		{"02:00-06:00", "02:00", true, "2026-11-09 06:00"},
		{"02:00-06:00", "05:59", true, "2026-11-09 06:00"},
		{"02:00-06:00", "06:00", false, ""},
		{"02:00-06:00", "14:00", false, ""},
		{"23:00-05:00", "23:30", true, "2026-11-10 05:00"},
		{"23:00-05:00", "01:00", true, "2026-11-09 05:00"},
		{"23:00-05:00", "12:00", false, ""},
	}
	for _, tt := range tests {
		w, err := ParseWindow(tt.window)
		if err != nil {
			t.Fatal(err)
		}
		end, inside := w.End(at(tt.now))
		if inside != tt.inside || (inside && end.Format("2006-01-02 15:04") != tt.end) {
			t.Errorf("%s at %s: inside=%v end=%v; want %v %s", tt.window, tt.now, inside, end, tt.inside, tt.end)
		}
	}
	for _, bad := range []string{"2-6", "02:00", "25:00-03:00", "02:00-02:00"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Errorf("ParseWindow(%q) should fail", bad)
		}
	}
}

func TestGuard(t *testing.T) {
	ctx := context.Background()
	reading := GPU{UtilPct: 1, UsedMiB: 300}
	read := func(context.Context) (GPU, error) { return reading, nil }

	gd, err := claim(ctx, read)
	if err != nil {
		t.Fatalf("an idle GPU should be claimable: %v", err)
	}

	reading = GPU{UtilPct: 95, UsedMiB: 300 + 900} // our model loaded: 900 MiB
	if err := gd.Check(ctx, 900); err != nil {
		t.Errorf("our own load shouldn't count: %v", err)
	}
	reading.UsedMiB += 3000 // a game starts
	if err := gd.Check(ctx, 900); err == nil {
		t.Error("a game taking 3 GiB should stop the job")
	}

	for _, busy := range []GPU{{UtilPct: 80, UsedMiB: 300}, {UtilPct: 5, UsedMiB: 4000}} {
		reading = busy
		if _, err := claim(ctx, read); err == nil {
			t.Errorf("%+v: shouldn't start on a busy GPU", busy)
		}
	}
}

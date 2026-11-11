package overdrive

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestAvailability(t *testing.T) {
	lib := newFakeLibrary(t, 0)
	c := NewClient(Config{Library: "test", BaseURL: lib.server.URL, RequestsPerSecond: 1000, RetryDelay: time.Millisecond})
	var ids []string
	for i := range 250 {
		ids = append(ids, strconv.Itoa(i))
	}
	avail, err := c.Availability(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(avail) != 250 || avail[249].TitleID != "249" || !avail[0].Available {
		t.Errorf("got %d snapshots, last %+v", len(avail), avail[len(avail)-1])
	}
	if got := lib.bulkRequests.Load(); got != 3 {
		t.Errorf("%d bulk requests for 250 titles, want 3 (100 per request)", got)
	}
}

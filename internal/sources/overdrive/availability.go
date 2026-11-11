package overdrive

import (
	"context"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// BulkMax is how many ids one bulk request takes.
const BulkMax = 100

// Availability fetches the current availability of the given titles,
// BulkMax to a request: far cheaper than a crawl when only a few hundred
// titles matter between crawls. Titles Thunder doesn't return are left
// out. On an error it returns what it fetched before it.
func (c *Client) Availability(ctx context.Context, ids []string) ([]contract.Availability, error) {
	var out []contract.Availability
	for chunk := range slices.Chunk(ids, BulkMax) {
		full, err := c.bulk(ctx, chunk)
		if err != nil {
			return out, err
		}
		for _, m := range full {
			out = append(out, availabilityOf(m))
		}
	}
	return out, nil
}

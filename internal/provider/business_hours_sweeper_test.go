package provider_test

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// sweepBusinessHours deletes business hours whose name starts with sweepPrefix.
// It can run in any order relative to components: deleting business hours
// detaches them from their components (the server sets the link to null).
//
// Alarm notes need no sweeper: they are deleted with their alarm (database
// cascade), and alarms are swept with their monitors.
func sweepBusinessHours(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/businessHours?page%5Bsize%5D=500", &doc); err != nil {
		return 0, fmt.Errorf("listing business_hours: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/businessHours/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete business_hours %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted business_hours %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

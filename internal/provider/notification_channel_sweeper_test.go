package provider_test

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// sweepNotificationChannels deletes notification channels whose name starts
// with sweepPrefix. Notification policies and escalation steps that reference
// a channel should be swept before this runs.
func sweepNotificationChannels(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/notificationChannels", &doc); err != nil {
		return 0, fmt.Errorf("listing notification_channels: %w", err)
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
		if err := c.delete(ctx, "/notificationChannels/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete notification_channel %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted notification_channel %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

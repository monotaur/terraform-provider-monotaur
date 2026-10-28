package provider_test

// calendar_event_sweeper_test.go holds the sweeper for monotaur_calendar_event.
// It is wired into runAllSweepers in sweeper_test.go.
//
// Order: before labels. Calendar events block nothing (deleting an event or a
// label only removes the join rows), but sweeping events first leaves no event
// pointing at a label that is about to go away.

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// calendarEventSweepDoc is a minimal collection document for calendar events,
// which are identified by their title.
type calendarEventSweepDoc struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes *struct {
			Title *string `json:"title,omitempty"`
		} `json:"attributes,omitempty"`
	} `json:"data"`
}

// sweepCalendarEvents deletes calendar events whose title starts with sweepPrefix.
func sweepCalendarEvents(ctx context.Context, c *sweepClient) (int, error) {
	var doc calendarEventSweepDoc
	if err := c.get(ctx, "/calendarEvents?"+notificationSweepPageSize, &doc); err != nil {
		return 0, fmt.Errorf("listing calendar_events: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Attributes == nil || res.Attributes.Title == nil || !strings.HasPrefix(*res.Attributes.Title, sweepPrefix) {
			continue
		}
		title := *res.Attributes.Title
		if err := c.delete(ctx, "/calendarEvents/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete calendar_event %s (%s): %v", title, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted calendar_event %s (%s)", title, res.ID)
		deleted++
	}
	return deleted, nil
}

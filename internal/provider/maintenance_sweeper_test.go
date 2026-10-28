package provider_test

// maintenance_sweeper_test.go holds the sweepers for
// monotaur_maintenance_schedule and monotaur_maintenance_window.
//
// Order: maintenance schedules → maintenance windows, both before components.
// Deleting a schedule removes its recurring job (so no new window is generated
// mid-sweep) and deletes its not-yet-started generated window; started and past
// generated windows are kept with their schedule reference cleared. Generated
// windows copy the schedule's description, so the windows pass then catches
// them along with one-off test windows. Neither blocks deleting a component
// (the join rows cascade), but sweeping them first leaves no window pointing
// at a component that is about to go away.

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// maintenanceSweepPageSize lists up to the API's maximum page size (the
// default is 10). `page[size]` is percent-encoded.
const maintenanceSweepPageSize = "page%5Bsize%5D=500"

// maintenanceSweepDoc is a minimal collection document for resources that are
// identified by a description rather than a name.
type maintenanceSweepDoc struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes *struct {
			Description *string `json:"description,omitempty"`
		} `json:"attributes,omitempty"`
	} `json:"data"`
}

// sweepMaintenanceByDescription deletes the resources at collection (e.g.
// "/maintenanceWindows") whose description starts with sweepPrefix. kind is
// used in log messages.
func sweepMaintenanceByDescription(ctx context.Context, c *sweepClient, collection, kind string) (int, error) {
	var doc maintenanceSweepDoc
	if err := c.get(ctx, collection+"?"+maintenanceSweepPageSize, &doc); err != nil {
		return 0, fmt.Errorf("listing %ss: %w", kind, err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Attributes == nil || res.Attributes.Description == nil || !strings.HasPrefix(*res.Attributes.Description, sweepPrefix) {
			continue
		}
		desc := *res.Attributes.Description
		if err := c.delete(ctx, collection+"/"+res.ID); err != nil {
			if strings.Contains(err.Error(), "HTTP 404") {
				// Already gone, e.g. a schedule's upcoming window removed with it.
				continue
			}
			log.Printf("[WARN] sweeper: failed to delete %s %s (%s): %v", kind, desc, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted %s %s (%s)", kind, desc, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepMaintenanceSchedules deletes maintenance schedules whose description
// starts with sweepPrefix. Run it before sweepMaintenanceWindows.
func sweepMaintenanceSchedules(ctx context.Context, c *sweepClient) (int, error) {
	return sweepMaintenanceByDescription(ctx, c, "/maintenanceSchedules", "maintenance_schedule")
}

// sweepMaintenanceWindows deletes maintenance windows (one-off and generated)
// whose description starts with sweepPrefix.
func sweepMaintenanceWindows(ctx context.Context, c *sweepClient) (int, error) {
	return sweepMaintenanceByDescription(ctx, c, "/maintenanceWindows", "maintenance_window")
}

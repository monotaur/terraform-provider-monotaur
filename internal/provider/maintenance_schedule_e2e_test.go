package provider_test

// maintenance_schedule_e2e_test.go contains end-to-end acceptance tests for
// the monotaur_maintenance_schedule resource and data source. Tests are
// guarded by TF_ACC=1 and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY.
//
// Covered acceptance criteria:
//   - create (every attribute and a component) → data source reads it back,
//     and the API generates the upcoming window with the schedule's
//     description and components
//   - import by ID (component_ids round-trip through `include`)
//   - update: description, cron, flags, a non-canonical TimeSpan ("2:0:0",
//     returned as "02:00:00" and kept as written), clear time_zone_id (explicit
//     null) and component_ids (`data: []`)
//   - plan-only: re-planning the updated config is empty
//   - destroy: the schedule and its not-yet-started generated window are gone
//   - descriptions use acctest.Name() so the sweeper matches them.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestAccMonotaurMaintenanceSchedule_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	componentName := acctest.Name("component", "msched1")
	description := acctest.Name("maintenance-schedule", "1")
	updatedDescription := acctest.Name("maintenance-schedule", "1-upd")

	const addr = "monotaur_maintenance_schedule.test"
	const dsAddr = "data.monotaur_maintenance_schedule.test"

	var scheduleID, generatedWindowID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			ctx := context.Background()
			c, err := newSweepClient()
			if err != nil {
				return err
			}
			if scheduleID != "" {
				if err := c.get(ctx, "/maintenanceSchedules/"+scheduleID, &struct{}{}); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
					return fmt.Errorf("maintenance schedule %s still exists after destroy (err=%v)", scheduleID, err)
				}
			}
			if generatedWindowID != "" {
				if err := c.get(ctx, "/maintenanceWindows/"+generatedWindowID, &struct{}{}); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
					return fmt.Errorf("upcoming generated window %s was not deleted with its schedule (err=%v)", generatedWindowID, err)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			// Step 1: create with every attribute and one component.
			{
				Config: testAccMaintenanceScheduleConfigFull(componentName, description),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "description", description),
					resource.TestCheckResourceAttr(addr, "schedule", "0 3 * * 0"),
					resource.TestCheckResourceAttr(addr, "time_span", "01:30:00"),
					resource.TestCheckResourceAttr(addr, "time_zone_id", "Europe/Berlin"),
					resource.TestCheckResourceAttr(addr, "exclude_from_downtime", "true"),
					resource.TestCheckResourceAttr(addr, "apply_to_component_status", "true"),
					resource.TestCheckResourceAttr(addr, "squelch", "true"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttr(addr, "component_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "component_ids.0", "monotaur_component.c1", "id"),
					// Data source mirrors the resource.
					resource.TestCheckResourceAttrPair(dsAddr, "id", addr, "id"),
					resource.TestCheckResourceAttr(dsAddr, "description", description),
					resource.TestCheckResourceAttr(dsAddr, "time_span", "01:30:00"),
					resource.TestCheckResourceAttr(dsAddr, "time_zone_id", "Europe/Berlin"),
					resource.TestCheckResourceAttr(dsAddr, "squelch", "true"),
					resource.TestCheckResourceAttr(dsAddr, "component_ids.#", "1"),
					resource.TestCheckResourceAttrPair(dsAddr, "component_ids.0", "monotaur_component.c1", "id"),
					// The API materialised the upcoming window.
					testAccCheckMaintenanceScheduleGeneratedWindow(addr, description, &scheduleID, &generatedWindowID),
				),
			},
			// Step 2: import by ID.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: change everything, clear time_zone_id and component_ids. The
			// component stays in config so it is not deleted while referenced.
			{
				Config: testAccMaintenanceScheduleConfigCleared(componentName, updatedDescription),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "description", updatedDescription),
					resource.TestCheckResourceAttr(addr, "schedule", "30 1 * * *"),
					resource.TestCheckResourceAttr(addr, "time_span", "2:0:0"),
					resource.TestCheckNoResourceAttr(addr, "time_zone_id"),
					resource.TestCheckResourceAttr(addr, "exclude_from_downtime", "false"),
					resource.TestCheckResourceAttr(addr, "apply_to_component_status", "false"),
					resource.TestCheckResourceAttr(addr, "squelch", "false"),
					resource.TestCheckResourceAttr(addr, "component_ids.#", "0"),
				),
			},
			// Step 4: re-planning the same config must be empty (TimeSpan
			// normalisation and the cleared values do not show as drift).
			{
				Config:   testAccMaintenanceScheduleConfigCleared(componentName, updatedDescription),
				PlanOnly: true,
			},
			// Step 5: import again; the API holds the canonical TimeSpan spelling.
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"time_span"},
			},
		},
	})
}

// testAccCheckMaintenanceScheduleGeneratedWindow checks that the API generated
// exactly one upcoming window for the schedule, carrying its description and
// components, and records the schedule and window IDs for CheckDestroy.
func testAccCheckMaintenanceScheduleGeneratedWindow(addr, description string, scheduleID, windowID *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not found in state", addr)
		}
		*scheduleID = rs.Primary.ID
		componentID := rs.Primary.Attributes["component_ids.0"]

		ctx := context.Background()
		c, err := newSweepClient()
		if err != nil {
			return err
		}

		var rel struct {
			Data []resourceID `json:"data"`
		}
		if err := c.get(ctx, "/maintenanceSchedules/"+rs.Primary.ID+"/relationships/maintenanceWindows", &rel); err != nil {
			return err
		}
		if len(rel.Data) != 1 {
			return fmt.Errorf("expected 1 generated window, got %d", len(rel.Data))
		}
		*windowID = rel.Data[0].ID

		var win struct {
			Data struct {
				Attributes struct {
					Description string `json:"description"`
				} `json:"attributes"`
				Relationships struct {
					Components struct {
						Data []resourceID `json:"data"`
					} `json:"components"`
				} `json:"relationships"`
			} `json:"data"`
		}
		if err := c.get(ctx, "/maintenanceWindows/"+*windowID+"?include=components", &win); err != nil {
			return err
		}
		if win.Data.Attributes.Description != description {
			return fmt.Errorf("generated window description: want %q, got %q", description, win.Data.Attributes.Description)
		}
		comps := win.Data.Relationships.Components.Data
		if len(comps) != 1 || comps[0].ID != componentID {
			return fmt.Errorf("generated window components: want [%s], got %v", componentID, comps)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func testAccMaintenanceScheduleConfigFull(componentName, description string) string {
	return fmt.Sprintf(`
resource "monotaur_component" "c1" {
  name = %q
}

resource "monotaur_maintenance_schedule" "test" {
  description               = %q
  schedule                  = "0 3 * * 0"
  time_span                 = "01:30:00"
  time_zone_id              = "Europe/Berlin"
  exclude_from_downtime     = true
  apply_to_component_status = true
  squelch                   = true
  component_ids             = [monotaur_component.c1.id]
}

data "monotaur_maintenance_schedule" "test" {
  id = monotaur_maintenance_schedule.test.id
}
`, componentName, description)
}

func testAccMaintenanceScheduleConfigCleared(componentName, description string) string {
	return fmt.Sprintf(`
resource "monotaur_component" "c1" {
  name = %q
}

resource "monotaur_maintenance_schedule" "test" {
  description               = %q
  schedule                  = "30 1 * * *"
  time_span                 = "2:0:0"
  exclude_from_downtime     = false
  apply_to_component_status = false
  squelch                   = false
  component_ids             = []
}
`, componentName, description)
}

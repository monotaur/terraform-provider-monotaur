package provider_test

// maintenance_window_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_maintenance_window resource and data source. Tests are guarded by
// TF_ACC=1 and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY.
//
// Covered acceptance criteria:
//   - an end before the start is rejected at plan time
//   - create (every attribute, UTC timestamps) → data source reads it back
//   - import by ID
//   - update: description, reason, flags, and timestamps written with a
//     +02:00 offset and fractional seconds (the API returns them in UTC; the
//     configured spelling is kept)
//   - plan-only: re-planning the updated config is empty
//   - destroy
//   - descriptions use acctest.Name() so the sweeper matches them.

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestAccMonotaurMaintenanceWindow_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	description := acctest.Name("maintenance-window", "1")
	updatedDescription := acctest.Name("maintenance-window", "1-upd")

	const addr = "monotaur_maintenance_window.test"
	const dsAddr = "data.monotaur_maintenance_window.test"

	updatedStart := "2031-06-01T22:00:00+02:00"
	updatedEnd := "2031-06-02T01:30:00.25+02:00"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: an end before the start fails validation before any API call.
			{
				Config: testAccMaintenanceWindowConfig(description, "2031-01-01T10:00:00Z", "2031-01-01T09:00:00Z",
					"ScheduledMaintenance", false),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must not be before start_date_time`),
			},
			// Step 2: create with every attribute.
			{
				Config: testAccMaintenanceWindowConfig(description, "2031-01-01T10:00:00Z", "2031-01-01T12:00:00Z",
					"EmergencyMaintenance", true) + testAccMaintenanceWindowDataSource(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "description", description),
					resource.TestCheckResourceAttr(addr, "start_date_time", "2031-01-01T10:00:00Z"),
					resource.TestCheckResourceAttr(addr, "end_date_time", "2031-01-01T12:00:00Z"),
					resource.TestCheckResourceAttr(addr, "reason", "EmergencyMaintenance"),
					resource.TestCheckResourceAttr(addr, "exclude_from_downtime", "true"),
					resource.TestCheckResourceAttr(addr, "apply_to_component_status", "true"),
					resource.TestCheckResourceAttr(addr, "squelch", "true"),
					resource.TestCheckNoResourceAttr(addr, "maintenance_schedule_id"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttrPair(dsAddr, "id", addr, "id"),
					resource.TestCheckResourceAttr(dsAddr, "description", description),
					resource.TestCheckResourceAttr(dsAddr, "start_date_time", "2031-01-01T10:00:00Z"),
					resource.TestCheckResourceAttr(dsAddr, "end_date_time", "2031-01-01T12:00:00Z"),
					resource.TestCheckResourceAttr(dsAddr, "reason", "EmergencyMaintenance"),
					resource.TestCheckResourceAttr(dsAddr, "squelch", "true"),
					resource.TestCheckNoResourceAttr(dsAddr, "maintenance_schedule_id"),
				),
			},
			// Step 3: import by ID.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 4: update everything; timestamps use an offset and fractional
			// seconds and are kept as written.
			{
				Config: testAccMaintenanceWindowConfig(updatedDescription, updatedStart, updatedEnd, "ExternalIssue", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "description", updatedDescription),
					resource.TestCheckResourceAttr(addr, "start_date_time", updatedStart),
					resource.TestCheckResourceAttr(addr, "end_date_time", updatedEnd),
					resource.TestCheckResourceAttr(addr, "reason", "ExternalIssue"),
					resource.TestCheckResourceAttr(addr, "exclude_from_downtime", "false"),
					resource.TestCheckResourceAttr(addr, "apply_to_component_status", "false"),
					resource.TestCheckResourceAttr(addr, "squelch", "false"),
				),
			},
			// Step 5: re-planning the same config must be empty.
			{
				Config:   testAccMaintenanceWindowConfig(updatedDescription, updatedStart, updatedEnd, "ExternalIssue", false),
				PlanOnly: true,
			},
			// Step 6: import again; the API returns the same instants in UTC.
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"start_date_time", "end_date_time"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported instance, got %d", len(states))
					}
					want := map[string]string{
						"start_date_time": "2031-06-01T20:00:00Z",
						"end_date_time":   "2031-06-01T23:30:00.25Z",
					}
					for k, v := range want {
						if got := states[0].Attributes[k]; got != v {
							return fmt.Errorf("imported %s: want %q, got %q", k, v, got)
						}
					}
					return nil
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func testAccMaintenanceWindowConfig(description, start, end, reason string, flags bool) string {
	return fmt.Sprintf(`
resource "monotaur_maintenance_window" "test" {
  description               = %q
  start_date_time           = %q
  end_date_time             = %q
  reason                    = %q
  exclude_from_downtime     = %t
  apply_to_component_status = %t
  squelch                   = %t
}
`, description, start, end, reason, flags, flags, flags)
}

func testAccMaintenanceWindowDataSource() string {
	return `
data "monotaur_maintenance_window" "test" {
  id = monotaur_maintenance_window.test.id
}
`
}

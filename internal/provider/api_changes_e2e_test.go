package provider_test

// api_changes_e2e_test.go covers attributes Monotaur added to existing
// resources: alarm severity and acknowledgement, monitor status rule priority,
// probe time zone and component status.

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurAPIChanges_newAttributes
//
// Steps:
//  1. Create a component, monitor, alarm (severity Warning), status rule
//     (priority 5) and probe (time zone Europe/Berlin). Assert the new
//     attributes, including the computed component status.
//  2. Import each resource and verify round-trip equality.
//  3. Change severity and priority and remove the time zone; the API must
//     clear it (sent as an explicit null).
//  4. Re-apply: the plan must be empty.
// ---------------------------------------------------------------------------

func TestAccMonotaurAPIChanges_newAttributes(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	componentName := acctest.Name("component", "api")
	monitorName := acctest.Name("monitor", "api")

	cfg := func(severity string, priority int, timeZone string) string {
		tz := ""
		if timeZone != "" {
			tz = fmt.Sprintf("\n  time_zone_id = %q", timeZone)
		}
		return fmt.Sprintf(`
resource "monotaur_component" "test" {
  name = %q
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_alarm" "test" {
  monitor_id = monotaur_monitor.test.id
  severity   = %q
}

resource "monotaur_monitor_status_rule" "test" {
  monitor_id = monotaur_monitor.test.id
  predicate  = "probe.status == 'Fault'"
  status     = "Fault"
  priority   = %d
}

resource "monotaur_probe" "test" {
  monitor_id = monotaur_monitor.test.id
  schedule   = "*/5 * * * *"%s
}
`, componentName, monitorName, severity, priority, tz)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("Warning", 5, "Europe/Berlin"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_alarm.test", "severity", "Warning"),
					resource.TestCheckNoResourceAttr("monotaur_alarm.test", "acknowledged_at"),
					resource.TestCheckNoResourceAttr("monotaur_alarm.test", "acknowledged_by"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "priority", "5"),
					resource.TestCheckResourceAttr("monotaur_probe.test", "time_zone_id", "Europe/Berlin"),
					resource.TestCheckResourceAttrSet("monotaur_component.test", "status"),
				),
			},
			{
				ResourceName:            "monotaur_alarm.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"update_date_time"},
			},
			{
				// monitor_id is ignored as in TestAccMonotaurMonitorStatusRule_basic.
				ResourceName:            "monotaur_monitor_status_rule.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"monitor_id"},
			},
			{
				ResourceName:            "monotaur_probe.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"sensor_ids"},
			},
			{
				Config: cfg("Info", 1, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_alarm.test", "severity", "Info"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "priority", "1"),
					resource.TestCheckNoResourceAttr("monotaur_probe.test", "time_zone_id"),
				),
			},
			{
				Config: cfg("Info", 1, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

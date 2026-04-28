package provider_test

// alarm_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_alarm resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurAlarm_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (exclude_from_downtime + squelch) → delete
//   - Drift detection: out-of-band PATCH squelch via raw HTTP, plan detects non-empty diff
//   - Changing monitor_id triggers an in-place update (no RequiresReplace on monitor_id)
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers
//   - No assumptions about an empty backend (pre-existing resources are ignored)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurAlarm_basic: full lifecycle
//
// Steps:
//  1. Create label → component → monitor → alarm. Set exclude_from_downtime
//     and squelch to false. Assert id, monitor_id, create_date_time, and
//     update_date_time are populated in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update exclude_from_downtime = true and squelch = true with explicit
//     start_date_time and end_date_time values. Assert all changed attributes
//     land in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurAlarm_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "alarm-basic")
	componentName := acctest.Name("component", "alarm-basic")
	monitorName := acctest.Name("monitor", "alarm-basic")

	// Fixed RFC3339 timestamps used in the update step.
	startDT := "2025-01-01T00:00:00Z"
	endDT := "2025-01-02T00:00:00Z"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the full hierarchy and the alarm. Assert all
			// settable and computed attributes land in state as expected.
			{
				Config: testAccAlarmConfig(
					labelName, componentName, monitorName,
					false, false, "", "",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "monitor_id"),
					resource.TestCheckResourceAttr("monotaur_alarm.test", "exclude_from_downtime", "false"),
					resource.TestCheckResourceAttr("monotaur_alarm.test", "squelch", "false"),
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "update_date_time"),
				),
			},
			// Step 2: Import by ID — verify all state attributes round-trip correctly.
			{
				ResourceName:      "monotaur_alarm.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update all settable attributes. Assert changes land in state.
			{
				Config: testAccAlarmConfig(
					labelName, componentName, monitorName,
					true, true, startDT, endDT,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_alarm.test", "exclude_from_downtime", "true"),
					resource.TestCheckResourceAttr("monotaur_alarm.test", "squelch", "true"),
					resource.TestCheckResourceAttr("monotaur_alarm.test", "start_date_time", startDT),
					resource.TestCheckResourceAttr("monotaur_alarm.test", "end_date_time", endDT),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurAlarm_drift: out-of-band change detection
//
// Steps:
//  1. Create the hierarchy and alarm with squelch = false. Capture the alarm ID.
//  2. PATCH squelch = true out-of-band via raw API (simulating an operator
//     change outside Terraform). Refresh state and assert that Terraform detects
//     a non-empty plan — the provider's Read must surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurAlarm_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "alarm-drift")
	componentName := acctest.Name("component", "alarm-drift")
	monitorName := acctest.Name("monitor", "alarm-drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the alarm and capture its API-assigned ID.
			{
				Config: testAccAlarmConfig(
					labelName, componentName, monitorName,
					false, false, "", "",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_alarm.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_alarm.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_alarm.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate squelch out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := alarmOutOfBandPatchSquelch(capturedID, true); err != nil {
						t.Fatalf("drift test: out-of-band PATCH failed: %v", err)
					}
				},
				// RefreshState re-reads the live API state into Terraform state without
				// applying the config. PostRefresh plan checks then assert drift.
				RefreshState: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectNonEmptyPlan(),
					},
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurAlarm_monitorChange: changing monitor_id triggers in-place update
//
// The monitor_id attribute has no RequiresReplace plan modifier — changing it
// triggers an in-place update (PATCH) rather than destroy + create. This test
// asserts that the framework plans an Update action for the alarm when
// monitor_id changes from monitor1 to monitor2.
//
// Steps:
//  1. Create label → component → monitor1 + monitor2 → alarm attached to
//     monitor1. Assert monitor_id == monitor1.id.
//  2. Reconfigure to use monitor2. Assert that Terraform plans an Update
//     (in-place) for the alarm, not a destroy-before-create. Assert
//     monitor_id == monitor2.id after apply.
// ---------------------------------------------------------------------------

func TestAccMonotaurAlarm_monitorChange(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "alarm-monchg")
	componentName := acctest.Name("component", "alarm-monchg")
	monitor1Name := acctest.Name("monitor", "alarm-monchg-1")
	monitor2Name := acctest.Name("monitor", "alarm-monchg-2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create alarm attached to monitor1. Assert monitor_id is monitor1.
			{
				Config: testAccAlarmTwoMonitorsConfig(
					labelName, componentName, monitor1Name, monitor2Name, "monitor1",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_alarm.test", "id"),
					resource.TestCheckResourceAttrPair(
						"monotaur_alarm.test", "monitor_id",
						"monotaur_monitor.monitor1", "id",
					),
				),
			},
			// Step 2: Reconfigure to monitor2. Because monitor_id has no RequiresReplace,
			// the framework should plan an in-place Update.
			{
				Config: testAccAlarmTwoMonitorsConfig(
					labelName, componentName, monitor1Name, monitor2Name, "monitor2",
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_alarm.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"monotaur_alarm.test", "monitor_id",
						"monotaur_monitor.monitor2", "id",
					),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccAlarmConfig returns a Terraform configuration that creates the full
// hierarchy: label → component → monitor → alarm. When startDateTime or
// endDateTime are empty they are omitted from the HCL so the API assigns
// defaults.
func testAccAlarmConfig(labelName, componentName, monitorName string, excludeFromDowntime, squelch bool, startDateTime, endDateTime string) string {
	startBlock := ""
	if startDateTime != "" {
		startBlock = fmt.Sprintf("\n  start_date_time      = %q", startDateTime)
	}
	endBlock := ""
	if endDateTime != "" {
		endBlock = fmt.Sprintf("\n  end_date_time        = %q", endDateTime)
	}

	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}

resource "monotaur_component" "test" {
  name      = %q
  label_ids = [monotaur_label.test.id]
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_alarm" "test" {
  monitor_id            = monotaur_monitor.test.id
  exclude_from_downtime = %t
  squelch               = %t%s%s
}
`, labelName, componentName, monitorName, excludeFromDowntime, squelch, startBlock, endBlock)
}

// testAccAlarmTwoMonitorsConfig returns a Terraform configuration that creates
// two monitors under the same component, allowing the alarm's monitor_id to be
// re-pointed between them. activeMonitor must be either "monitor1" or "monitor2" —
// it names the Terraform resource whose id is used as monitor_id.
func testAccAlarmTwoMonitorsConfig(labelName, componentName, monitor1Name, monitor2Name, activeMonitor string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}

resource "monotaur_component" "test" {
  name      = %q
  label_ids = [monotaur_label.test.id]
}

resource "monotaur_monitor" "monitor1" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_monitor" "monitor2" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_alarm" "test" {
  monitor_id = monotaur_monitor.%s.id
}
`, labelName, componentName, monitor1Name, monitor2Name, activeMonitor)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// alarmOutOfBandPatchSquelch performs a raw JSON:API PATCH on /alarms/{id} to
// change only the squelch field, simulating an operator change outside Terraform.
// The provider does not see the change until the next Read/Refresh.
func alarmOutOfBandPatchSquelch(id string, squelch bool) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "alarms",
			"id":   id,
			"attributes": map[string]interface{}{
				"squelch": squelch,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/alarms/%s", endpoint, id)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create PATCH request: %w", err)
	}
	const ct = "application/vnd.api+json; ext=openapi"
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute PATCH: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PATCH /alarms/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

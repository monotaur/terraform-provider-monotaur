package provider_test

// monitor_status_rule_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_monitor_status_rule resource. Tests are guarded by TF_ACC=1 and
// require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurMonitorStatusRule_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (all settable attributes) → delete
//   - Drift detection: out-of-band PATCH via raw HTTP, plan detects non-empty diff
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

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurMonitorStatusRule_basic: full lifecycle
//
// Steps:
//  1. Create label → component → monitor → monitor_status_rule. Assert all
//     settable attributes are reflected in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update predicate, status, and status_message to new values. Assert all
//     changed attributes are reflected in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitorStatusRule_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.LabelText("sr-basic")
	componentName := acctest.Name("component", "sr-basic")
	monitorName := acctest.Name("monitor", "sr-basic")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the full hierarchy and the status rule. Assert all
			// settable attributes land in state as expected.
			{
				Config: testAccMonitorStatusRuleConfig(
					labelName, componentName, monitorName,
					"probe.status == 'Fault'",
					"Fault",
					"probe is unreachable",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "id"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "predicate", "probe.status == 'Fault'"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "status", "Fault"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "status_message", "probe is unreachable"),
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "monitor_id"),
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "update_date_time"),
				),
			},
			// Step 2: Import by ID — verify state attributes round-trip. monitor_id
			// is excluded because JSON:API GETs return relationships as `links`-only
			// (no `data`), so the imported state can't populate the relationship ID.
			{
				ResourceName:            "monotaur_monitor_status_rule.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"monitor_id"},
			},
			// Step 3: Update every settable attribute. Assert all changes land in state.
			{
				Config: testAccMonitorStatusRuleConfig(
					labelName, componentName, monitorName,
					"probe.status == 'Warning'",
					"Warning",
					"probe is slow",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "predicate", "probe.status == 'Warning'"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "status", "Warning"),
					resource.TestCheckResourceAttr("monotaur_monitor_status_rule.test", "status_message", "probe is slow"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurMonitorStatusRule_drift: out-of-band change detection
//
// Steps:
//  1. Create the hierarchy and status rule. Capture the rule ID.
//  2. PATCH predicate out-of-band via raw API (simulating an operator change
//     outside Terraform). Refresh state and assert that Terraform detects a
//     non-empty plan — the provider's Read must surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitorStatusRule_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.LabelText("sr-drift")
	componentName := acctest.Name("component", "sr-drift")
	monitorName := acctest.Name("monitor", "sr-drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the status rule and capture its API-assigned ID.
			{
				Config: testAccMonitorStatusRuleConfig(
					labelName, componentName, monitorName,
					"probe.status == 'Fault'",
					"Fault",
					"probe is down",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_monitor_status_rule.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_monitor_status_rule.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_monitor_status_rule.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate predicate out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := monitorStatusRuleOutOfBandPatch(capturedID, "probe.status == 'Warning'"); err != nil {
						t.Fatalf("drift test: out-of-band PATCH failed: %v", err)
					}
				},
				// RefreshState re-reads the live API state into Terraform state without
				// applying the config. PostRefresh plan checks then assert drift.
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
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
// TestAccMonotaurMonitorStatusRule_monitorReplace: changing monitor_id
//
// The monitor_id attribute has no RequiresReplace plan modifier — changing it
// triggers an in-place update (PATCH) rather than destroy + create. This test
// asserts that the framework plans an Update action for the status rule when
// monitor_id changes from monitor_1 to monitor_2.
//
// Steps:
//  1. Create status rule attached to monitor_1.
//  2. Reconfigure to use monitor_2. Assert that Terraform plans an Update
//     (in-place) for the status rule, not a destroy-before-create.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitorStatusRule_monitorReplace(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.LabelText("sr-mrep")
	componentName := acctest.Name("component", "sr-mrep")
	monitorName1 := acctest.Name("monitor", "sr-mrep-1")
	monitorName2 := acctest.Name("monitor", "sr-mrep-2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create with monitor_1.
			{
				Config: testAccMonitorStatusRuleTwoMonitorsConfig(
					labelName, componentName,
					monitorName1, monitorName2,
					"monitor_1", // which monitor the rule uses
					"probe.status == 'Fault'",
					"Fault",
					"",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_monitor_status_rule.test", "id"),
					resource.TestCheckResourceAttrPair(
						"monotaur_monitor_status_rule.test", "monitor_id",
						"monotaur_monitor.monitor_1", "id",
					),
				),
			},
			// Step 2: Reconfigure to monitor_2. Because monitor_id has no
			// RequiresReplace, the framework should plan an in-place Update.
			{
				Config: testAccMonitorStatusRuleTwoMonitorsConfig(
					labelName, componentName,
					monitorName1, monitorName2,
					"monitor_2", // now pointing at monitor_2
					"probe.status == 'Fault'",
					"Fault",
					"",
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_monitor_status_rule.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"monotaur_monitor_status_rule.test", "monitor_id",
						"monotaur_monitor.monitor_2", "id",
					),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccMonitorStatusRuleConfig returns a Terraform configuration that creates
// the full hierarchy: label → component → monitor → monitor_status_rule.
// status and statusMessage may be empty strings; when empty they are omitted
// from the HCL so the API assigns defaults.
func testAccMonitorStatusRuleConfig(labelName, componentName, monitorName, predicate, status, statusMessage string) string {
	statusBlock := ""
	if status != "" {
		statusBlock = fmt.Sprintf("\n  status         = %q", status)
	}

	statusMessageBlock := ""
	if statusMessage != "" {
		statusMessageBlock = fmt.Sprintf("\n  status_message = %q", statusMessage)
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

resource "monotaur_monitor_status_rule" "test" {
  monitor_id = monotaur_monitor.test.id
  predicate  = %q%s%s
}
`, labelName, componentName, monitorName, predicate, statusBlock, statusMessageBlock)
}

// testAccMonitorStatusRuleTwoMonitorsConfig returns a Terraform configuration
// that creates two monitors under one component, allowing the status rule to be
// re-pointed between them. activeMonitor must be either "monitor_1" or
// "monitor_2" — it names the Terraform resource whose id is used as monitor_id.
func testAccMonitorStatusRuleTwoMonitorsConfig(labelName, componentName, monitorName1, monitorName2, activeMonitor, predicate, status, statusMessage string) string {
	statusBlock := ""
	if status != "" {
		statusBlock = fmt.Sprintf("\n  status         = %q", status)
	}

	statusMessageBlock := ""
	if statusMessage != "" {
		statusMessageBlock = fmt.Sprintf("\n  status_message = %q", statusMessage)
	}

	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}

resource "monotaur_component" "test" {
  name      = %q
  label_ids = [monotaur_label.test.id]
}

resource "monotaur_monitor" "monitor_1" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_monitor" "monitor_2" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_monitor_status_rule" "test" {
  monitor_id = monotaur_monitor.%s.id
  predicate  = %q%s%s
}
`, labelName, componentName, monitorName1, monitorName2, activeMonitor, predicate, statusBlock, statusMessageBlock)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// monitorStatusRuleOutOfBandPatch performs a raw JSON:API PATCH on
// /monitor-status-rules/{id} to change only the predicate field, simulating an
// operator change outside Terraform. The provider does not see the change until
// the next Read/Refresh.
func monitorStatusRuleOutOfBandPatch(id, newPredicate string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "monitorStatusRules",
			"id":   id,
			"attributes": map[string]interface{}{
				"predicate": newPredicate,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/monitorStatusRules/%s", endpoint, id)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create PATCH request: %w", err)
	}
	const ct = "application/vnd.api+json; ext=openapi"
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute PATCH: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PATCH /api/v1/monitorStatusRules/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

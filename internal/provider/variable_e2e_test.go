package provider_test

// variable_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_variable resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurVariable_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (name, value, description) → delete
//   - Drift detection: out-of-band PATCH on value, plan detects non-empty diff
//   - monitor_id change: verify changing monitor_id is an in-place update (not destroy+recreate)
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
// TestAccMonotaurVariable_basic: full lifecycle
//
// Steps:
//  1. Create a variable without a monitor (monitor_id is optional). Assert all
//     settable and computed attributes are present in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update name, value, and description. Assert all changed attributes are
//     reflected in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurVariable_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	varName := acctest.Name("variable", "1")
	updatedName := acctest.Name("variable", "1-upd")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create a variable without a monitor. Assert all expected
			// attributes are set in state.
			{
				Config: testAccVariableConfigBasic(varName, "initial-value", "initial description"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_variable.test", "id"),
					resource.TestCheckResourceAttr("monotaur_variable.test", "name", varName),
					resource.TestCheckResourceAttr("monotaur_variable.test", "value", "initial-value"),
					resource.TestCheckResourceAttr("monotaur_variable.test", "description", "initial description"),
					resource.TestCheckResourceAttrSet("monotaur_variable.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_variable.test", "update_date_time"),
				),
			},
			// Step 2: Import by ID — verify round-trip equality of all state attributes.
			{
				ResourceName:      "monotaur_variable.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update name, value, and description. Assert all changes land
			// in state.
			{
				Config: testAccVariableConfigBasic(updatedName, "updated-value", "updated description"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_variable.test", "name", updatedName),
					resource.TestCheckResourceAttr("monotaur_variable.test", "value", "updated-value"),
					resource.TestCheckResourceAttr("monotaur_variable.test", "description", "updated description"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurVariable_drift: out-of-band change detection
//
// Steps:
//  1. Create a variable and capture its API-assigned ID.
//  2. In PreConfig, PATCH the value out-of-band via the raw API (simulating a
//     manual change outside Terraform). Then perform a RefreshState step and
//     assert that Terraform detects a non-empty plan — the provider's Read must
//     surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurVariable_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	varName := acctest.Name("variable", "drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the variable and capture its API-assigned ID.
			{
				Config: testAccVariableConfigBasic(varName, "original-value", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_variable.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_variable.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_variable.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_variable.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate the value out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					oobValue := acctest.Name("variable", "drift-oob")
					if err := variableOutOfBandPatch(capturedID, oobValue); err != nil {
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
// TestAccMonotaurVariable_monitorChange: monitor_id in-place update
//
// The monitor_id attribute has no RequiresReplace plan modifier — changing it
// triggers an in-place update (PATCH) rather than destroy + create. This test
// asserts that the framework plans an Update action for the variable when
// monitor_id changes from monitor_1 to monitor_2.
//
// Steps:
//  1. Create label → component → two monitors → variable attached to monitor_1.
//     Assert monitor_id is set and matches monitor_1.
//  2. Reconfigure the variable to use monitor_2. Assert that Terraform plans an
//     Update (in-place) for the variable, not a destroy-before-create. Assert
//     the updated monitor_id matches monitor_2 after apply.
// ---------------------------------------------------------------------------

func TestAccMonotaurVariable_monitorChange(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "var-mc")
	componentName := acctest.Name("component", "var-mc")
	monitorName1 := acctest.Name("monitor", "var-mc-1")
	monitorName2 := acctest.Name("monitor", "var-mc-2")
	varName := acctest.Name("variable", "var-mc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the full hierarchy and variable attached to monitor_1.
			// Assert monitor_id is populated and matches monitor_1.
			{
				Config: testAccVariableWithTwoMonitorsConfig(
					labelName, componentName,
					monitorName1, monitorName2,
					varName, "monitor_1",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_variable.test", "id"),
					resource.TestCheckResourceAttrPair(
						"monotaur_variable.test", "monitor_id",
						"monotaur_monitor.monitor_1", "id",
					),
				),
			},
			// Step 2: Reconfigure the variable to use monitor_2. Because monitor_id
			// has no RequiresReplace, the framework should plan an in-place Update.
			{
				Config: testAccVariableWithTwoMonitorsConfig(
					labelName, componentName,
					monitorName1, monitorName2,
					varName, "monitor_2",
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_variable.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"monotaur_variable.test", "monitor_id",
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

// testAccVariableConfigBasic returns a Terraform configuration for a
// monotaur_variable without a monitor_id. Pass an empty string for description
// to omit the attribute so the API assigns a default.
func testAccVariableConfigBasic(name, value, description string) string {
	descriptionBlock := ""
	if description != "" {
		descriptionBlock = fmt.Sprintf("\n  description = %q", description)
	}

	return fmt.Sprintf(`
resource "monotaur_variable" "test" {
  name  = %q
  value = %q%s
}
`, name, value, descriptionBlock)
}

// testAccVariableWithTwoMonitorsConfig returns a Terraform configuration that
// creates two monitors under one component and a variable that references one of
// them. activeMonitor must be either "monitor_1" or "monitor_2" — it names the
// Terraform resource whose id is used as monitor_id.
func testAccVariableWithTwoMonitorsConfig(labelName, componentName, monitorName1, monitorName2, varName, activeMonitor string) string {
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

resource "monotaur_variable" "test" {
  name       = %q
  value      = "monitor-change-value"
  monitor_id = monotaur_monitor.%s.id
}
`, labelName, componentName, monitorName1, monitorName2, varName, activeMonitor)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// variableOutOfBandPatch performs a raw JSON:API PATCH on /variables/{id} to
// change only the value field, simulating a manual operator change outside
// Terraform. The provider does not see the change until the next Read/Refresh.
func variableOutOfBandPatch(id, newValue string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "variables",
			"id":   id,
			"attributes": map[string]interface{}{
				"value": newValue,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/variables/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /variables/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

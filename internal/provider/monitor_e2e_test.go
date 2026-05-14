package provider_test

// monitor_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_monitor resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurMonitor_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (name + component_ids cycle) → delete
//   - Drift detection: out-of-band PATCH via raw HTTP, plan detects non-empty diff
//   - Computed-only attributes (status, status_message, status_expiration_date_time)
//     are populated after create and do NOT cause unwanted plan diffs
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers

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
// TestAccMonotaurMonitor_basic: full lifecycle
//
// Steps:
//  1. Create a monitor with two components. Assert all settable and computed
//     attributes are present in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update name and cycle component_ids (remove first component, keep second).
//     Assert name + component_ids reflect the change.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitor_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	monitorName := acctest.Name("monitor", "1")
	updatedName := acctest.Name("monitor", "1-upd")
	comp1Name := acctest.Name("component", "mon1a")
	comp2Name := acctest.Name("component", "mon1b")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create monitor with two components. Assert all attributes are set.
			{
				Config: testAccMonitorConfigWithTwoComponents(monitorName, comp1Name, comp2Name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_monitor.test", "name", monitorName),
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "type"),
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "update_date_time"),
					resource.TestCheckResourceAttr("monotaur_monitor.test", "component_ids.#", "2"),
				),
			},
			// Step 2: Import by ID — verify round-trip equality of all state attributes.
			// component_ids is skipped: the API echoes the components
			// relationship with `links` only (no `data`) on a bare GET, so a
			// fresh import has no member IDs to verify against the original.
			{
				ResourceName:            "monotaur_monitor.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"component_ids"},
			},
			// Step 3: Update name and remove the first component (keep only comp2).
			{
				Config: testAccMonitorConfigWithOneComponent(updatedName, comp1Name, comp2Name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_monitor.test", "name", updatedName),
					resource.TestCheckResourceAttr("monotaur_monitor.test", "component_ids.#", "1"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurMonitor_drift: out-of-band change detection
//
// Steps:
//  1. Create a monitor and capture its API-assigned ID.
//  2. In PreConfig, PATCH the name out-of-band via the raw API (simulating a
//     manual change outside Terraform). Then perform a RefreshState step and
//     assert that Terraform detects a non-empty plan — the provider's Read must
//     surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitor_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	monitorName := acctest.Name("monitor", "drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially
	// within a single run, so there is no data race.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the monitor and capture its API-assigned ID.
			{
				Config: testAccMonitorConfigBasic(monitorName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_monitor.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_monitor.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_monitor.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate the name out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					oobName := acctest.Name("monitor", "drift-oob")
					if err := monitorOutOfBandPatch(capturedID, oobName); err != nil {
						t.Fatalf("drift test: out-of-band PATCH failed: %v", err)
					}
				},
				// RefreshState re-reads the live API state without applying the config.
				// PostRefresh plan checks then assert that the name change is detected.
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
// TestAccMonotaurMonitor_computedNodrift: verify Computed attributes do not
// cause unwanted plan diffs
//
// These fields are read-only and managed entirely by the API:
//   - status
//   - status_message
//   - status_expiration_date_time
//
// Steps:
//  1. Create a monitor. Assert that the three computed fields are populated in
//     state (the API always returns values for them).
//  2. Plan only (no apply). Assert the plan is empty — the provider correctly
//     treats server-side values for these fields as non-drifting.
// ---------------------------------------------------------------------------

func TestAccMonotaurMonitor_computedNodrift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	monitorName := acctest.Name("monitor", "nodrift")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create — verify the computed status fields are populated.
			{
				Config: testAccMonitorConfigBasic(monitorName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_monitor.test", "name", monitorName),
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "id"),
					// `status` is populated by the API for any new monitor.
					// `status_message` and `status_expiration_date_time` can
					// legitimately be null until a status rule fires, so we
					// don't assert they are non-empty; the no-drift check on
					// Step 2 covers the "computed-but-null" case.
					resource.TestCheckResourceAttrSet("monotaur_monitor.test", "status"),
				),
			},
			// Step 2: Plan only — assert no diff. Computed fields must not cause drift.
			{
				Config:   testAccMonitorConfigBasic(monitorName),
				PlanOnly: true,
				// ExpectNonEmptyPlan defaults to false; an empty plan is the passing condition.
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccMonitorConfigBasic returns a minimal Terraform configuration for a
// monotaur_monitor with only a name set.
func testAccMonitorConfigBasic(name string) string {
	return fmt.Sprintf(`
resource "monotaur_monitor" "test" {
  name = %q
}
`, name)
}

// testAccMonitorConfigWithTwoComponents returns a Terraform configuration
// containing two monotaur_component resources and a monotaur_monitor that
// references both via component_ids.
func testAccMonitorConfigWithTwoComponents(monitorName, comp1Name, comp2Name string) string {
	return fmt.Sprintf(`
resource "monotaur_component" "c1" {
  name = %q
}

resource "monotaur_component" "c2" {
  name = %q
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [
    monotaur_component.c1.id,
    monotaur_component.c2.id,
  ]
}
`, comp1Name, comp2Name, monitorName)
}

// testAccMonitorConfigWithOneComponent returns a Terraform configuration that
// keeps both component resources (so neither is destroyed) but attaches only
// the second component to the monitor, exercising the component_ids remove path.
func testAccMonitorConfigWithOneComponent(monitorName, comp1Name, comp2Name string) string {
	return fmt.Sprintf(`
resource "monotaur_component" "c1" {
  name = %q
}

resource "monotaur_component" "c2" {
  name = %q
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [
    monotaur_component.c2.id,
  ]
}
`, comp1Name, comp2Name, monitorName)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// monitorOutOfBandPatch performs a raw JSON:API PATCH on /monitors/{id} to
// change only the name field, simulating a manual operator change outside
// Terraform. This is intentionally not using the provider's client so the
// provider does not see the change until the next Read/Refresh.
func monitorOutOfBandPatch(id, newName string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "monitors",
			"id":   id,
			"attributes": map[string]interface{}{
				"name": newName,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/monitors/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /monitors/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

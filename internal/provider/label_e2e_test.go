package provider_test

// label_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_label resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurLabel_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (color + icon + text) → delete
//   - Drift detection: out-of-band PATCH via raw HTTP, plan detects non-empty diff
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
// TestAccMonotaurLabel_basic: full lifecycle
//
// Steps:
//  1. Create with text + color. Assert all attributes are set.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update color, icon, and text. Assert all changed attributes reflect
//     the new values in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurLabel_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	name := acctest.Name("label", "basic")
	updatedName := acctest.Name("label", "basic-upd")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create — verify all settable attributes are present in state.
			{
				Config: testAccLabelConfig(name, "#FF0000", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_label.test", "text", name),
					resource.TestCheckResourceAttr("monotaur_label.test", "color", "#FF0000"),
					resource.TestCheckResourceAttrSet("monotaur_label.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_label.test", "name"),
					resource.TestCheckResourceAttrSet("monotaur_label.test", "value"),
					resource.TestCheckResourceAttrSet("monotaur_label.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_label.test", "update_date_time"),
				),
			},
			// Step 2: Import — verify round-trip equality of all state attributes.
			{
				ResourceName:      "monotaur_label.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update text, color, and set an icon — verify all changes land in state.
			{
				Config: testAccLabelConfigWithIcon(updatedName, "#00FF00", "🚩"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_label.test", "text", updatedName),
					resource.TestCheckResourceAttr("monotaur_label.test", "color", "#00FF00"),
					resource.TestCheckResourceAttr("monotaur_label.test", "icon", "🚩"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurLabel_drift: out-of-band change detection
//
// Steps:
//  1. Create with color #FF0000. Capture the resource ID into a closure var.
//  2. In PreConfig, PATCH the color to #0000FF via the raw API (simulating an
//     operator change outside Terraform). Then perform a refresh-only step and
//     assert that Terraform detects a non-empty plan — the provider's Read
//     must surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurLabel_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	name := acctest.Name("label", "drift")

	// capturedID is populated by the Check function in step 1 and consumed in
	// the PreConfig of step 2. Using a pointer-to-string avoids a data race
	// because the framework calls Check and PreConfig in sequence, never
	// concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the label and capture its API-assigned ID.
			{
				Config: testAccLabelConfig(name, "#FF0000", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_label.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_label.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_label.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_label.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate the color out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						// PreCheck should have caught missing credentials; if we
						// reach here with no ID the create step failed.
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := labelOutOfBandPatch(capturedID, "#0000FF"); err != nil {
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
// Config helpers
// ---------------------------------------------------------------------------

// testAccLabelConfig returns a Terraform configuration for a monotaur_label
// resource with the given text and color. Pass an empty string for color to
// omit the attribute (the API will assign a default).
func testAccLabelConfig(text, color, _ string) string {
	if color == "" {
		return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}
`, text)
	}
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text  = %q
  color = %q
}
`, text, color)
}

// testAccLabelConfigWithIcon returns a Terraform configuration for a
// monotaur_label resource with text, color, and icon all set.
func testAccLabelConfigWithIcon(text, color, icon string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text  = %q
  color = %q
  icon  = %q
}
`, text, color, icon)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// labelOutOfBandPatch performs a raw JSON:API PATCH on /labels/{id} to change
// only the color field, simulating a manual operator change outside Terraform.
// This is intentionally not using the provider's client so the provider does
// not see the change until the next Read/Refresh.
func labelOutOfBandPatch(id, newColor string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "labels",
			"id":   id,
			"attributes": map[string]interface{}{
				"color": newColor,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/labels/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /labels/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

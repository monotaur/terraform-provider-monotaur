package provider_test

// role_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_role resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurRole_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (name + description + permissions) → delete
//   - Drift detection: out-of-band PATCH on name, plan detects non-empty diff
//   - Permissions cycling: start with 1 permission, add 1, remove back to 1
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
// TestAccMonotaurRole_basic: full lifecycle
//
// Steps:
//  1. Create with name, description = "initial", and permissions = ["read:monitors"].
//     Assert id, name, and permissions are set in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update name, description, and permissions (add "read:labels"). Assert all
//     changed attributes land in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurRole_basic(t *testing.T) {
	name := acctest.Name("role", "1")
	updatedName := acctest.Name("role", "1-upd")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create — verify all settable attributes are present in state.
			{
				Config: testAccRoleConfig(name, "initial", []string{"read:monitors"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role.test", "id"),
					resource.TestCheckResourceAttr("monotaur_role.test", "name", name),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.#", "1"),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.0", "read:monitors"),
				),
			},
			// Step 2: Import — verify round-trip equality of all state attributes.
			{
				ResourceName:      "monotaur_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update name, description, and permissions — verify all changes
			// land in state.
			{
				Config: testAccRoleConfig(updatedName, "updated", []string{"read:monitors", "read:labels"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_role.test", "name", updatedName),
					resource.TestCheckResourceAttr("monotaur_role.test", "description", "updated"),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.#", "2"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurRole_drift: out-of-band change detection
//
// Steps:
//  1. Create with name = acctest.Name("role", "drift"). Capture the role ID.
//  2. In PreConfig, PATCH the name to acctest.Name("role", "drift-oob") via the
//     raw API (simulating an operator change outside Terraform). Then perform a
//     refresh-only step and assert that Terraform detects a non-empty plan —
//     the provider's Read must surface the mutated name attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurRole_drift(t *testing.T) {
	name := acctest.Name("role", "drift")

	// capturedID is populated by the Check function in step 1 and consumed in
	// the PreConfig of step 2. The framework calls Check and PreConfig
	// sequentially, never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the role and capture its API-assigned ID.
			{
				Config: testAccRoleConfig(name, "drift test role", []string{"read:monitors"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_role.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_role.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_role.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate the name out-of-band, then refresh state and assert that
			// Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						// PreCheck should have caught missing credentials; if we
						// reach here with no ID the create step failed.
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					oobName := acctest.Name("role", "drift-oob")
					if err := roleOutOfBandPatchName(capturedID, oobName); err != nil {
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
// TestAccMonotaurRole_permissionsCycle: add and remove permissions
//
// Steps:
//  1. Create with permissions = ["read:monitors"]. Assert permissions.# = 1.
//  2. Add "read:labels" → permissions = ["read:monitors", "read:labels"]. Assert
//     permissions.# = 2.
//  3. Remove "read:monitors" → permissions = ["read:labels"]. Assert
//     permissions.# = 1 and permissions.0 = "read:labels".
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurRole_permissionsCycle(t *testing.T) {
	name := acctest.Name("role", "permcycle")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create with one permission. Assert count and value.
			{
				Config: testAccRoleConfig(name, "permissions cycle test", []string{"read:monitors"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role.test", "id"),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.#", "1"),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.0", "read:monitors"),
				),
			},
			// Step 2: Add a second permission. Assert count increases to 2.
			{
				Config: testAccRoleConfig(name, "permissions cycle test", []string{"read:monitors", "read:labels"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.#", "2"),
				),
			},
			// Step 3: Remove the first permission, keeping only "read:labels". Assert
			// count returns to 1 and the remaining permission is "read:labels".
			{
				Config: testAccRoleConfig(name, "permissions cycle test", []string{"read:labels"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.#", "1"),
					resource.TestCheckResourceAttr("monotaur_role.test", "permissions.0", "read:labels"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccRoleConfig returns a Terraform configuration for a monotaur_role
// resource with the given name, description, and permissions list. Description
// is always set; pass an empty string to send an explicit empty description.
func testAccRoleConfig(name, description string, permissions []string) string {
	permItems := make([]string, len(permissions))
	for i, p := range permissions {
		permItems[i] = fmt.Sprintf("%q", p)
	}
	return fmt.Sprintf(`
resource "monotaur_role" "test" {
  name        = %q
  description = %q
  permissions = [%s]
}
`, name, description, strings.Join(permItems, ", "))
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// roleOutOfBandPatchName performs a raw JSON:API PATCH on /admin/roles/{id} to
// change only the name field, simulating a manual operator change outside
// Terraform. This is intentionally not using the provider's client so the
// provider does not see the change until the next Read/Refresh.
func roleOutOfBandPatchName(id, newName string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "admin.roles",
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

	url := fmt.Sprintf("%s/admin/roles/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /admin/roles/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

package provider_test

// role_assignment_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_role_assignment resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurRoleAssignment_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → replace (new role + new service account) → delete
//   - Drift detection: out-of-band DELETE via raw HTTP, RefreshState detects non-empty plan
//   - Service accounts are created via raw HTTP API (no monotaur_service_account resource)
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers
//   - No assumptions about an empty backend (pre-existing resources are ignored)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
// Service account helper
// ---------------------------------------------------------------------------

// createServiceAccount creates a service account via the raw admin API and
// returns its ID along with a cleanup function that deletes it. The caller
// must call the returned cleanup (typically via defer) to avoid leaving test
// resources behind.
//
// Always uses a dedicated http.Client with a 30-second timeout; never uses
// http.DefaultClient.
func createServiceAccount(name string) (id string, cleanup func(), err error) {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return "", nil, fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "admin.serviceAccounts",
			"attributes": map[string]interface{}{
				"name": name,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal create body: %w", err)
	}

	const ct = "application/vnd.api+json; ext=openapi"
	url := fmt.Sprintf("%s/admin/service-accounts", endpoint)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("execute POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("POST /admin/service-accounts: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("decode create response: %w", err)
	}
	if result.Data.ID == "" {
		return "", nil, fmt.Errorf("create service account returned empty ID")
	}

	saID := result.Data.ID
	cleanupFn := func() {
		deleteURL := fmt.Sprintf("%s/admin/service-accounts/%s", endpoint, saID)
		delReq, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
		if err != nil {
			return
		}
		delReq.Header.Set("Accept", ct)
		delReq.Header.Set("Authorization", "Bearer "+apiKey)

		delClient := &http.Client{Timeout: 30 * time.Second}
		delResp, err := delClient.Do(delReq)
		if err != nil {
			return
		}
		delResp.Body.Close()
	}

	return saID, cleanupFn, nil
}

// ---------------------------------------------------------------------------
// TestAccMonotaurRoleAssignment_basic: full lifecycle
//
// Steps:
//  1. Create a service account (via raw API) + a monotaur_role + a
//     monotaur_role_assignment. Assert id, role_id, service_account_id, and
//     assigned_at are populated in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Create a second service account and a second role, then update the
//     config to use new_role_id + new_service_account_id. Assert that
//     Terraform plans a DestroyBeforeCreate (replace) for the role assignment.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurRoleAssignment_basic(t *testing.T) {
	roleName := acctest.Name("role", "ra-basic")
	secondRoleName := acctest.Name("role", "ra-basic-2")

	saID, saCleanup, err := createServiceAccount(acctest.Name("sa", "ra-basic"))
	if err != nil {
		t.Fatalf("failed to create service account: %v", err)
	}
	defer saCleanup()

	saID2, saCleanup2, err := createServiceAccount(acctest.Name("sa", "ra-basic-2"))
	if err != nil {
		t.Fatalf("failed to create second service account: %v", err)
	}
	defer saCleanup2()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create role + role_assignment. Assert all attributes are set.
			{
				Config: testAccRoleAssignmentConfig(roleName, saID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "role_id"),
					resource.TestCheckResourceAttr("monotaur_role_assignment.test", "service_account_id", saID),
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "assigned_at"),
				),
			},
			// Step 2: Import by ID — verify round-trip equality of all state attributes.
			{
				ResourceName:      "monotaur_role_assignment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Change both role_id and service_account_id. Because both attributes
			// are marked RequiresReplace, the framework must plan a DestroyBeforeCreate.
			{
				Config: testAccRoleAssignmentReplaceConfig(roleName, secondRoleName, saID, saID2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_role_assignment.test",
							plancheck.ResourceActionDestroyBeforeCreate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "id"),
					resource.TestCheckResourceAttr("monotaur_role_assignment.test", "service_account_id", saID2),
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "assigned_at"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurRoleAssignment_drift: out-of-band delete detection
//
// Steps:
//  1. Create a service account (via raw API) + a monotaur_role + a
//     monotaur_role_assignment. Capture the assignment ID in a closure.
//  2. In PreConfig, DELETE the role assignment via the raw API (simulating an
//     operator change outside Terraform). Then perform a RefreshState step and
//     assert that Terraform detects a non-empty plan — the provider's Read must
//     surface the missing resource and mark it for recreation.
// ---------------------------------------------------------------------------

func TestAccMonotaurRoleAssignment_drift(t *testing.T) {
	roleName := acctest.Name("role", "ra-drift")

	saID, saCleanup, err := createServiceAccount(acctest.Name("sa", "ra-drift"))
	if err != nil {
		t.Fatalf("failed to create service account: %v", err)
	}
	defer saCleanup()

	// capturedID is populated by the Check function in step 1 and consumed in
	// the PreConfig of step 2. The framework calls Check and PreConfig in
	// sequence, never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the role + role_assignment and capture its API-assigned ID.
			{
				Config: testAccRoleAssignmentConfig(roleName, saID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_role_assignment.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_role_assignment.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_role_assignment.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_role_assignment.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Delete the role assignment out-of-band, then refresh state and
			// assert that Terraform plans a non-empty diff to recreate it.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := roleAssignmentOutOfBandDelete(capturedID); err != nil {
						t.Fatalf("drift test: out-of-band DELETE failed: %v", err)
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

// testAccRoleAssignmentConfig returns a Terraform configuration containing a
// monotaur_role and a monotaur_role_assignment that binds it to a pre-existing
// service account identified by saID. The service account is created via the
// raw API before the test (not managed by Terraform).
func testAccRoleAssignmentConfig(roleName, saID string) string {
	return fmt.Sprintf(`
resource "monotaur_role" "test" {
  name        = %q
  permissions = ["read:monitors"]
}

resource "monotaur_role_assignment" "test" {
  role_id            = monotaur_role.test.id
  service_account_id = %q
}
`, roleName, saID)
}

// testAccRoleAssignmentReplaceConfig returns a Terraform configuration
// containing two monotaur_role resources (keeping the original role so it is
// not destroyed) and a monotaur_role_assignment that references the second role
// and the second service account. This exercises the RequiresReplace semantics
// on both role_id and service_account_id.
func testAccRoleAssignmentReplaceConfig(roleName, secondRoleName, _, saID2 string) string {
	return fmt.Sprintf(`
resource "monotaur_role" "test" {
  name        = %q
  permissions = ["read:monitors"]
}

resource "monotaur_role" "test2" {
  name        = %q
  permissions = ["read:monitors"]
}

resource "monotaur_role_assignment" "test" {
  role_id            = monotaur_role.test2.id
  service_account_id = %q
}
`, roleName, secondRoleName, saID2)
}

// ---------------------------------------------------------------------------
// Out-of-band deletion helper
// ---------------------------------------------------------------------------

// roleAssignmentOutOfBandDelete performs a raw DELETE on
// /admin/role-assignments/{id}, simulating an operator deletion outside
// Terraform. This is intentionally not using the provider's client so the
// provider does not see the removal until the next Read/Refresh.
func roleAssignmentOutOfBandDelete(id string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	url := fmt.Sprintf("%s/admin/role-assignments/%s", endpoint, id)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create DELETE request: %w", err)
	}
	const ct = "application/vnd.api+json; ext=openapi"
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute DELETE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE /admin/role-assignments/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

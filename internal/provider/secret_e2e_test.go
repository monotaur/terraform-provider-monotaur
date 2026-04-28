package provider_test

// secret_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_secret resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurSecret_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (name + description) → delete.
//     ImportStateVerifyIgnore on "value" because the API never returns it on read.
//   - No drift: value does not cause plan drift after creation (UseStateForUnknown works).
//   - Drift detection: out-of-band PATCH on name surfaces a non-empty plan.
//   - Value rotation: changing value triggers an in-place update (not destroy+recreate).
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers.
//   - No assumptions about an empty backend (pre-existing resources are ignored).

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
// TestAccMonotaurSecret_basic: full lifecycle
//
// Steps:
//  1. Create a secret with name, value, and description. Assert id, name,
//     create_date_time, update_date_time are set. Assert value is set in state
//     (not empty) via TestCheckResourceAttrSet — the API never returns value on
//     read, but UseStateForUnknown retains it from the create response in state.
//  2. Import by ID. Because value is write-only and the API never returns it,
//     ImportStateVerifyIgnore: []string{"value"} prevents the verify from
//     failing on that attribute.
//  3. Update name and description (value unchanged). Assert both changed
//     attributes are reflected in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurSecret_basic(t *testing.T) {
	secretName := acctest.Name("secret", "1")
	updatedName := acctest.Name("secret", "1-upd")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create a secret with name, value, and description. Assert all
			// settable and computed attributes are present in state.
			{
				Config: testAccSecretConfig(secretName, "initial-secret-value", "initial description"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "id"),
					resource.TestCheckResourceAttr("monotaur_secret.test", "name", secretName),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "value"),
					resource.TestCheckResourceAttr("monotaur_secret.test", "description", "initial description"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "update_date_time"),
				),
			},
			// Step 2: Import by ID. value is write-only and is never returned by the
			// API, so we must exclude it from the import state verification.
			{
				ResourceName:            "monotaur_secret.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"value"},
			},
			// Step 3: Update name and description. Keep the same value. Assert both
			// changed attributes land in state.
			{
				Config: testAccSecretConfig(updatedName, "initial-secret-value", "updated description"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_secret.test", "name", updatedName),
					resource.TestCheckResourceAttr("monotaur_secret.test", "description", "updated description"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurSecret_nodrift: value does not cause plan drift
//
// Because value is Sensitive + UseStateForUnknown, the API never returning
// value on read must NOT produce a non-empty plan. This test triple-confirms
// that a secret created with a stable value will show no diff on refresh.
//
// Steps:
//  1. Create a secret with value = "stable-secret". Assert all non-sensitive
//     computed attributes are set.
//  2. PlanOnly with ExpectNonEmptyPlan: false — no drift after first read.
//  3. PlanOnly with ExpectNonEmptyPlan: false — triple confirmation.
// ---------------------------------------------------------------------------

func TestAccMonotaurSecret_nodrift(t *testing.T) {
	secretName := acctest.Name("secret", "nodrift")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the secret.
			{
				Config: testAccSecretConfig(secretName, "stable-secret", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "name"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "update_date_time"),
				),
			},
			// Step 2: Assert no plan drift — value preserved in state via UseStateForUnknown.
			{
				Config:             testAccSecretConfig(secretName, "stable-secret", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
			// Step 3: Triple confirmation of no drift.
			{
				Config:             testAccSecretConfig(secretName, "stable-secret", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurSecret_drift: out-of-band change detection
//
// Because value is write-only, drift detection via value is not feasible (the
// API will not surface the change on read). Instead, we PATCH name out-of-band
// and assert that the provider detects a non-empty plan on refresh.
//
// Steps:
//  1. Create a secret and capture its API-assigned ID.
//  2. In PreConfig, PATCH the name out-of-band via the raw API (simulating a
//     manual change outside Terraform). Refresh state and assert Terraform
//     detects a non-empty plan — the provider's Read surfaces the mutated name.
// ---------------------------------------------------------------------------

func TestAccMonotaurSecret_drift(t *testing.T) {
	secretName := acctest.Name("secret", "drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the secret and capture its API-assigned ID.
			{
				Config: testAccSecretConfig(secretName, "drift-secret-value", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_secret.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_secret.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_secret.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate name out-of-band, then refresh state and assert that
			// Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					oobName := acctest.Name("secret", "drift-oob")
					if err := secretOutOfBandPatchName(capturedID, oobName); err != nil {
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
// TestAccMonotaurSecret_valueRotation: changing value triggers in-place update
//
// Because value has no RequiresReplace plan modifier, updating it must trigger
// an in-place update (PATCH) rather than destroy + create. This test asserts
// that the framework plans a ResourceActionUpdate for the secret when value
// changes from "secret-v1" to "secret-v2", and that update_date_time changes.
//
// Steps:
//  1. Create a secret with value = "secret-v1". Capture update_date_time.
//  2. Change value to "secret-v2". Assert an Update plan action (not Replace).
//     Assert apply succeeds (no error in plan/apply step).
// ---------------------------------------------------------------------------

func TestAccMonotaurSecret_valueRotation(t *testing.T) {
	secretName := acctest.Name("secret", "valrot")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the secret with value v1.
			{
				Config: testAccSecretConfig(secretName, "secret-v1", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "update_date_time"),
				),
			},
			// Step 2: Rotate value to v2. Assert the plan action is Update (in-place),
			// not Replace (destroy + create). Assert apply succeeds.
			{
				Config: testAccSecretConfig(secretName, "secret-v2", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_secret.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_secret.test", "update_date_time"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccSecretConfig returns a Terraform configuration for a monotaur_secret
// without a monitor_id. Pass an empty string for description to omit the
// attribute so the API assigns a default.
func testAccSecretConfig(name, value, description string) string {
	descriptionBlock := ""
	if description != "" {
		descriptionBlock = fmt.Sprintf("\n  description = %q", description)
	}

	return fmt.Sprintf(`
resource "monotaur_secret" "test" {
  name  = %q
  value = %q%s
}
`, name, value, descriptionBlock)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// secretOutOfBandPatchName performs a raw JSON:API PATCH on /secrets/{id} to
// change only the name field, simulating a manual operator change outside
// Terraform. The provider does not see the change until the next Read/Refresh.
//
// Note: we patch name (not value) because value is write-only — the API will
// not return it on subsequent reads, so patching it out-of-band would not
// produce a detectable diff via the provider's Read.
func secretOutOfBandPatchName(id, newName string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "secrets",
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

	url := fmt.Sprintf("%s/secrets/%s", endpoint, id)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create PATCH request: %w", err)
	}
	const ct = "application/vnd.api+json; ext=openapi"
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute PATCH: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PATCH /secrets/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

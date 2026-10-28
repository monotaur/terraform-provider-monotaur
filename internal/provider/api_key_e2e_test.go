package provider_test

// api_key_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_api_key resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurApiKey_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import (key_value ignored) → replace (RequiresReplace on name)
//   - No-drift: repeated plan-only steps verify key_value preserved via UseStateForUnknown
//   - Drift detection: out-of-band key revocation → RefreshState detects absence
//   - Service accounts are created via raw HTTP API (no monotaur_service_account resource)
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers
//   - No assumptions about an empty backend (pre-existing resources are ignored)

import (
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

// createServiceAccount is defined in e2e_helpers_test.go.

// ---------------------------------------------------------------------------
// TestAccMonotaurApiKey_basic: full lifecycle
//
// Steps:
//  1. Create a service account (via raw API) + a monotaur_api_key. Assert id,
//     name, environment, service_account_id, prefix, and key_value are set.
//  2. Import by ID. key_value is excluded from ImportStateVerify (the API does
//     not return it on GET — it's a write-once credential).
//  3. Change the name (RequiresReplace attribute) and assert Terraform plans a
//     DestroyBeforeCreate for the key.
// ---------------------------------------------------------------------------

func TestAccMonotaurApiKey_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	saID, saCleanup, err := createServiceAccount(acctest.Name("sa", "ak-basic"))
	if err != nil {
		t.Fatalf("failed to create service account: %v", err)
	}
	defer saCleanup()

	keyName := acctest.Name("apikey", "ak-basic")
	keyNameV2 := acctest.Name("apikey", "ak-basic-v2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create. Verify all readable attributes are populated.
			{
				Config: testAccApiKeyConfig(keyName, "test", saID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "id"),
					resource.TestCheckResourceAttr("monotaur_api_key.test", "name", keyName),
					resource.TestCheckResourceAttr("monotaur_api_key.test", "environment", "test"),
					resource.TestCheckResourceAttr("monotaur_api_key.test", "service_account_id", saID),
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "key_value"),
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "prefix"),
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "created_at"),
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "created_by"),
				),
			},
			// Step 2: Import by ID. key_value cannot be recovered from the API after
			// creation, so it is excluded from round-trip equality verification.
			{
				ResourceName:            "monotaur_api_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"key_value"},
			},
			// Step 3: Change the name. name is RequiresReplace — Terraform must plan
			// a DestroyBeforeCreate for the key.
			{
				Config: testAccApiKeyConfig(keyNameV2, "test", saID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_api_key.test",
							plancheck.ResourceActionDestroyBeforeCreate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_api_key.test", "name", keyNameV2),
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "key_value"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurApiKey_nodrift: write-once key_value persists across plans
//
// Steps:
//  1. Create an API key. Verify key_value is populated after create.
//  2. PlanOnly — assert no diff. Verify UseStateForUnknown preserves key_value.
//  3. PlanOnly again — assert no diff after a second read cycle.
// ---------------------------------------------------------------------------

func TestAccMonotaurApiKey_nodrift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	saID, saCleanup, err := createServiceAccount(acctest.Name("sa", "ak-nodrift"))
	if err != nil {
		t.Fatalf("failed to create service account: %v", err)
	}
	defer saCleanup()

	keyName := acctest.Name("apikey", "ak-nodrift")
	cfg := testAccApiKeyConfig(keyName, "test", saID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the key. Verify key_value is captured.
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "key_value"),
				),
			},
			// Step 2: Plan-only. The API does not return key_value on GET; UseStateForUnknown
			// must preserve the value stored at create time so no diff is shown.
			{
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
			// Step 3: Plan-only again to confirm no drift accumulates across multiple reads.
			{
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurApiKey_drift: out-of-band revocation detection
//
// Steps:
//  1. Create a service account (via raw API) + a monotaur_api_key. Capture the
//     key ID in a closure.
//  2. In PreConfig, revoke the key via the raw revoke endpoint (simulating an
//     operator change outside Terraform). Then perform a RefreshState step and
//     assert that Terraform detects a non-empty plan — the provider's Read must
//     treat the revoked key as gone and mark it for recreation.
// ---------------------------------------------------------------------------

func TestAccMonotaurApiKey_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	saID, saCleanup, err := createServiceAccount(acctest.Name("sa", "ak-drift"))
	if err != nil {
		t.Fatalf("failed to create service account: %v", err)
	}
	defer saCleanup()

	keyName := acctest.Name("apikey", "ak-drift")

	// capturedID is populated by the Check function in step 1 and consumed in
	// the PreConfig of step 2.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the key and capture its API-assigned ID.
			{
				Config: testAccApiKeyConfig(keyName, "test", saID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_api_key.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_api_key.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_api_key.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_api_key.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Revoke the key out-of-band, then refresh state and assert
			// non-empty plan.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := apiKeyOutOfBandRevoke(capturedID); err != nil {
						t.Fatalf("drift test: out-of-band revoke failed: %v", err)
					}
				},
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
// Config helpers
// ---------------------------------------------------------------------------

// testAccApiKeyConfig returns a Terraform configuration containing a
// monotaur_api_key for a pre-existing service account (created via raw API).
func testAccApiKeyConfig(name, environment, saID string) string {
	return fmt.Sprintf(`
resource "monotaur_api_key" "test" {
  name               = %q
  environment        = %q
  service_account_id = %q
}
`, name, environment, saID)
}

// ---------------------------------------------------------------------------
// Out-of-band revocation helper
// ---------------------------------------------------------------------------

// apiKeyOutOfBandRevoke revokes an API key, simulating an operator change
// outside Terraform. The provider uses the same endpoint in its Delete method.
//
// Endpoint: POST /api/v1/admin.apiKeys/{id}/revoke
func apiKeyOutOfBandRevoke(keyID string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	url := fmt.Sprintf("%s/api/v1/admin.apiKeys/%s/revoke", endpoint, keyID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create revoke request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute revoke: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke API key %s: HTTP %d: %s", keyID, resp.StatusCode, string(raw))
	}
	return nil
}

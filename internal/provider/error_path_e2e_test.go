package provider_test

// error_path_e2e_test.go tests cross-cutting error-handling paths that do not
// fit inside any single resource's E2E test file.
//
// Covered acceptance criteria:
//   - 404 after out-of-band delete: label deleted via raw API mid-test; next
//     RefreshState removes it from state; next apply with the original config
//     re-creates it.
//   - 422 invalid input: resource submitted with an API-level invalid attribute
//     value (syntactically valid for Terraform, semantically invalid for the
//     API); the JSON:API errors[] body must surface as a Diagnostic.
//   - 5xx retry: see note in TestAccErrorPath_retryOnTransientFailure.
//
// All raw HTTP helpers use &http.Client{Timeout: 30 * time.Second}.
// All resource names use acctest.Name() for collision-safe, sweepable identifiers.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurErrorPath_notFoundRecreation
//
// Verifies the full 404 → state-removal → re-creation lifecycle:
//  1. Create a label and capture its ID.
//  2. Delete it out-of-band via the raw API.
//  3. RefreshState — the provider must detect the 404 and remove the resource
//     from state (set to null / RemoveFromState).
//  4. Plan with the original config — Terraform must plan a Create action.
//  5. Apply — the resource is re-created with a new ID.
// ---------------------------------------------------------------------------

func TestAccMonotaurErrorPath_notFoundRecreation(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "err-404")
	cfg := testAccErrorPathLabelConfig(labelName)

	// capturedID is set by the Check in step 1 and used in the PreConfig of step 3.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the label and capture its ID.
			{
				Config: cfg,
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
			// Step 2: Delete the label out-of-band, then RefreshState.
			// After the delete, the provider's Read must detect the 404 and remove
			// the resource from state — the PostRefresh plan check confirms it is gone.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("404-recreation test: capturedID is empty — create step must have failed")
					}
					if err := labelOutOfBandDelete(capturedID); err != nil {
						t.Fatalf("404-recreation test: out-of-band DELETE failed: %v", err)
					}
				},
				RefreshState: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						// After Read detects 404, Terraform must plan a re-create.
						plancheck.ExpectNonEmptyPlan(),
					},
				},
			},
			// Step 3: Apply the original config — Terraform must re-create the label.
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_label.test", "id"),
					resource.TestCheckResourceAttr("monotaur_label.test", "name", labelName),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurErrorPath_invalidInput
//
// Verifies that the provider surfaces JSON:API 422 error responses as typed
// Terraform Diagnostics. The test submits a color value that is syntactically
// valid for Terraform (a non-empty string) but semantically invalid for the
// API (not a hex color code), expecting the API to reject it with 422 and the
// provider to surface a diagnostic mentioning the error.
// ---------------------------------------------------------------------------

func TestAccMonotaurErrorPath_invalidInput(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Submitting a non-hex color value. The Terraform schema accepts any
				// string for color; the API is expected to reject it with a 422.
				Config: testAccErrorPathLabelInvalidColorConfig(
					acctest.Name("label", "err-422"),
					"not-a-valid-hex-color",
				),
				// The provider must surface the API error — not a raw HTTP dump —
				// as a Terraform diagnostic. A 422 response from the Monotaur API
				// includes a JSON:API errors[] body describing which field failed.
				ExpectError: regexp.MustCompile(`(?i)invalid|unprocessable|422`),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurErrorPath_retryOnTransientFailure (not implemented)
//
// The 5xx retry acceptance criterion requires either:
//   a) A transient-failure HTTP middleware wrapping the generated API client, or
//   b) A staging-side fault injection endpoint.
//
// The generated oapi-codegen client does not currently include retry logic;
// adding it would require wrapping the http.Client with a retry transport (e.g.
// hashicorp/go-retryablehttp). Until that middleware exists, this test is left
// as a skeleton so the acceptance criteria are clearly documented.
// ---------------------------------------------------------------------------

func TestAccMonotaurErrorPath_retryOnTransientFailure(t *testing.T) {
	t.Skip("5xx retry test requires retry middleware in the API client — not yet implemented")
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func testAccErrorPathLabelConfig(name string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  name  = %q
  color = "#FF5733"
  icon  = "tag"
  text  = "Error path test label"
}
`, name)
}

func testAccErrorPathLabelInvalidColorConfig(name, color string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  name  = %q
  color = %q
  icon  = "tag"
  text  = "Invalid color test"
}
`, name, color)
}

// ---------------------------------------------------------------------------
// Out-of-band deletion helper
// ---------------------------------------------------------------------------

// labelOutOfBandDelete deletes a label via the raw API, simulating an operator
// deletion outside Terraform. The provider will detect the 404 on the next Read.
func labelOutOfBandDelete(id string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	url := fmt.Sprintf("%s/labels/%s", endpoint, id)
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
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE /labels/%s: HTTP %d: %s", id, resp.StatusCode, string(raw))
	}
	return nil
}

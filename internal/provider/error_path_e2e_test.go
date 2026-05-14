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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
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

	labelName := acctest.LabelText("err-404")
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
				// The framework runs an additional plan after the step; that
				// plan is also non-empty because the recreation hasn't been
				// applied yet (Step 3 does that).
				ExpectNonEmptyPlan: true,
			},
			// Step 3: Apply the original config — Terraform must re-create the label.
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_label.test", "id"),
					resource.TestCheckResourceAttr("monotaur_label.test", "text", labelName),
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
					acctest.LabelText("err-422"),
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
// TestAccMonotaurErrorPath_retryOnTransientFailure
//
// Verifies that the provider's HTTP transport retries transient failures (5xx
// on idempotent methods) automatically, so a flaky upstream surface as a
// transparent success rather than a Terraform diagnostic.
//
// The test spins up a local httptest.Server that serves a minimal subset of
// the Monotaur API and is programmed to return 503 a fixed number of times on
// the first GET /api/v1/labels/{id} before serving 200. The provider is
// pointed at that server via t.Setenv("MONOTAUR_ENDPOINT", ...).
//
// Steps:
//  1. Apply a config that creates a label. The stub serves POST immediately —
//     POSTs are intentionally not retried by the transport because the API
//     has no idempotency-key support.
//  2. RefreshState — explicitly triggers Read, which issues GET
//     /api/v1/labels/1. The stub returns 503 twice, then 200. The retry
//     transport must absorb the 5xxs so the refresh succeeds with no
//     diagnostic, and the stub's request counter must show 3+ attempts.
// ---------------------------------------------------------------------------

func TestAccMonotaurErrorPath_retryOnTransientFailure(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	const labelID = "1"
	const transient5xxCount = 2 // 503s served before the GET finally succeeds

	var getAttempts atomic.Int32
	labelBody := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "labels",
			"id":   labelID,
			"attributes": map[string]interface{}{
				"openapi:discriminator": "labels",
				"text":                  "retry-test-label",
				// Must match the value in testAccErrorPathLabelConfig so the
				// framework's post-apply consistency check passes.
				"color": "#FF5733",
			},
		},
	}
	labelBodyBytes, err := json.Marshal(labelBody)
	if err != nil {
		t.Fatalf("marshal label body: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/labels", func(w http.ResponseWriter, r *http.Request) {
		// Create endpoint — POSTs are NOT retried by design, so we always
		// respond successfully on the first call.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json; ext=openapi")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(labelBodyBytes)
	})
	mux.HandleFunc("/api/v1/labels/"+labelID, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			attempt := getAttempts.Add(1)
			if attempt <= transient5xxCount {
				// Mimic the API's JSON:API error body so that, if the retry
				// transport ever stops working, the failure surfaces with a
				// realistic-looking diagnostic instead of a parse error.
				w.Header().Set("Content-Type", "application/vnd.api+json; ext=openapi")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"errors":[{"status":"503","title":"transient","detail":"injected fault"}]}`)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.api+json; ext=openapi")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(labelBodyBytes)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Point the provider at the local stub. testAccPreCheck only checks that
	// the env vars are non-empty, so any value works for the API key.
	t.Setenv("MONOTAUR_ENDPOINT", srv.URL)
	t.Setenv("MONOTAUR_API_KEY", "stub-key")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Apply — POST creates the label. Not retried.
			{
				Config: testAccErrorPathLabelConfig("retry-test-label"),
				Check: resource.TestCheckResourceAttr("monotaur_label.test", "id", labelID),
			},
			// Step 2: Refresh — triggers GET. The stub returns 503 twice, then
			// 200; the retry transport must absorb the 5xxs so this step
			// succeeds with no diagnostic. The check asserts the stub saw the
			// expected number of attempts, proving retry actually fired.
			{
				RefreshState: true,
				Check: func(_ *terraform.State) error {
					if got := getAttempts.Load(); got < int32(transient5xxCount+1) {
						return fmt.Errorf("expected GET to be retried at least %d times, got %d attempts",
							transient5xxCount+1, got)
					}
					return nil
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func testAccErrorPathLabelConfig(text string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text  = %q
  color = "#FF5733"
}
`, text)
}

func testAccErrorPathLabelInvalidColorConfig(text, color string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text  = %q
  color = %q
}
`, text, color)
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

	url := fmt.Sprintf("%s/api/v1/labels/%s", endpoint, id)
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
		return fmt.Errorf("DELETE /api/v1/labels/%s: HTTP %d: %s", id, resp.StatusCode, string(raw))
	}
	return nil
}

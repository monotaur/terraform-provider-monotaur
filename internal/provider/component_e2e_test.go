package provider_test

// component_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_component resource. All tests are gated behind TF_ACC=1.
//
// To run:
//   TF_ACC=1 MONOTAUR_API_KEY=<key> MONOTAUR_ENDPOINT=<url> \
//     go test ./internal/provider/ -run 'TestAccMonotaurComponent' -v

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurComponent_basic
//
// Full lifecycle: create (with 2 labels) → import → update (name + label swap).
// ---------------------------------------------------------------------------

func TestAccMonotaurComponent_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	testAccPreCheck(t)

	componentName := acctest.Name("component", "1")
	componentNameUpdated := acctest.Name("component", "1u")
	label1Name := acctest.LabelText("1")
	label2Name := acctest.LabelText("2")
	label3Name := acctest.LabelText("3")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create component with two labels.
			{
				Config: testAccComponentConfig_withTwoLabels(
					label1Name, label2Name, componentName,
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "name", componentName,
					),
					resource.TestCheckResourceAttrSet(
						"monotaur_component.test", "id",
					),
					resource.TestCheckResourceAttrSet(
						"monotaur_component.test", "create_date_time",
					),
					resource.TestCheckResourceAttrSet(
						"monotaur_component.test", "update_date_time",
					),
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "label_ids.#", "2",
					),
				),
			},
			// Step 2: Import by ID — state should match prior config.
			// label_ids / monitor_ids / maintenance_window_ids are skipped:
			// the API echoes those relationships with `links` only (no `data`)
			// on a bare GET, so a fresh import has no member IDs to verify
			// against the original state.
			{
				ResourceName:      "monotaur_component.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"update_date_time",
					"label_ids",
					"monitor_ids",
					"maintenance_window_ids",
				},
			},
			// Step 3: Update — change name, remove label 2, add label 3.
			{
				Config: testAccComponentConfig_withUpdatedLabels(
					label1Name, label2Name, label3Name, componentNameUpdated,
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "name", componentNameUpdated,
					),
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "label_ids.#", "2",
					),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurComponent_drift
//
// Simulate an out-of-band rename via raw PATCH and assert that a subsequent
// plan detects the drift (ExpectNonEmptyPlan).
// ---------------------------------------------------------------------------

func TestAccMonotaurComponent_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	testAccPreCheck(t)

	componentName := acctest.Name("component", "drift")
	label1Name := acctest.LabelText("drift1")
	label2Name := acctest.LabelText("drift2")

	var capturedID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the component.
			{
				Config: testAccComponentConfig_withTwoLabels(
					label1Name, label2Name, componentName,
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_component.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_component.test"]
						if !ok {
							return fmt.Errorf("monotaur_component.test not found in state")
						}
						capturedID = rs.Primary.ID
						return nil
					},
				),
			},
			// Step 2: Rename the component out-of-band, then assert plan detects drift.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatal("capturedID is empty; component was not created in Step 1")
					}
					if err := patchComponentNameOutOfBand(capturedID, componentName+"-drifted"); err != nil {
						t.Fatalf("out-of-band PATCH failed: %v", err)
					}
				},
				Config:             testAccComponentConfig_withTwoLabels(label1Name, label2Name, componentName),
				ExpectNonEmptyPlan: true,
				// RefreshState causes Terraform to call Read before diffing.
				RefreshState: false,
				PlanOnly:     true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurComponent_labels
//
// Dedicated label-cycle test: create with 2 labels, update to remove one and
// add a third, verify the final API state has exactly the two expected labels.
// ---------------------------------------------------------------------------

func TestAccMonotaurComponent_labels(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	testAccPreCheck(t)

	componentName := acctest.Name("component", "labels")
	label1Name := acctest.LabelText("lblcyc1")
	label2Name := acctest.LabelText("lblcyc2")
	label3Name := acctest.LabelText("lblcyc3")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create with label 1 and label 2.
			{
				Config: testAccComponentConfig_withTwoLabels(
					label1Name, label2Name, componentName,
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "label_ids.#", "2",
					),
				),
			},
			// Step 2: Remove label 2, add label 3. Final state: label 1 + label 3.
			{
				Config: testAccComponentConfig_withUpdatedLabels(
					label1Name, label2Name, label3Name, componentName,
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						"monotaur_component.test", "label_ids.#", "2",
					),
					// label_ids is an ordered list; the config puts first at [0] and third at [1].
					resource.TestCheckResourceAttrPair(
						"monotaur_component.test", "label_ids.0",
						"monotaur_label.first", "id",
					),
					resource.TestCheckResourceAttrPair(
						"monotaur_component.test", "label_ids.1",
						"monotaur_label.third", "id",
					),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Terraform config helpers
// ---------------------------------------------------------------------------

// testAccComponentConfig_withTwoLabels creates three resources:
// monotaur_label.first, monotaur_label.second, and monotaur_component.test
// referencing both labels.
func testAccComponentConfig_withTwoLabels(label1Name, label2Name, componentName string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "first" {
  text  = %[1]q
  color = "#FF0000"
}

resource "monotaur_label" "second" {
  text  = %[2]q
  color = "#00FF00"
}

resource "monotaur_component" "test" {
  name      = %[3]q
  label_ids = [monotaur_label.first.id, monotaur_label.second.id]
}
`, label1Name, label2Name, componentName)
}

// testAccComponentConfig_withUpdatedLabels keeps label 1 and 3, drops label 2.
// The label 2 resource is still declared (for dependency ordering during
// destroy) but is no longer referenced in label_ids.
func testAccComponentConfig_withUpdatedLabels(label1Name, label2Name, label3Name, componentName string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "first" {
  text  = %[1]q
  color = "#FF0000"
}

resource "monotaur_label" "second" {
  text  = %[2]q
  color = "#00FF00"
}

resource "monotaur_label" "third" {
  text  = %[3]q
  color = "#0000FF"
}

resource "monotaur_component" "test" {
  name      = %[4]q
  label_ids = [monotaur_label.first.id, monotaur_label.third.id]
}
`, label1Name, label2Name, label3Name, componentName)
}

// ---------------------------------------------------------------------------
// Out-of-band API helpers for drift detection
// ---------------------------------------------------------------------------

// patchComponentNameOutOfBand sends a raw PATCH /components/{id} to rename
// the component without going through Terraform, simulating out-of-band drift.
func patchComponentNameOutOfBand(id, newName string) error {
	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	body, err := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"id":   id,
			"type": "components",
			"attributes": map[string]interface{}{
				"openapi:discriminator": "components",
				"name":                  newName,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/components/%s", endpoint, id)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.api+json; ext=openapi")
	req.Header.Set("Accept", "application/vnd.api+json; ext=openapi")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH /api/v1/components/%s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PATCH /api/v1/components/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

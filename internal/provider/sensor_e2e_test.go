package provider_test

// sensor_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_sensor resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurSensor_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (all settable attributes) → delete
//   - Drift detection: out-of-band PATCH via raw HTTP, plan detects non-empty diff
//   - Changing probe_id triggers an in-place update (no RequiresReplace on probe_id)
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
// TestAccMonotaurSensor_basic: full lifecycle
//
// Steps:
//  1. Create label → component → monitor → probe → sensor. Assert all
//     settable and computed attributes are reflected in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update name, plugin_name, type, and parameters. Assert all changed
//     attributes are reflected in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurSensor_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "sensor-basic")
	componentName := acctest.Name("component", "sensor-basic")
	monitorName := acctest.Name("monitor", "sensor-basic")
	sensorName := acctest.Name("sensor", "1")
	updatedSensorName := acctest.Name("sensor", "1-upd")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the full hierarchy and the sensor. Assert all
			// settable and computed attributes land in state as expected.
			{
				Config: testAccSensorConfig(
					labelName, componentName, monitorName,
					sensorName, "http", "HttpSensor", "",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "id"),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "name", sensorName),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "plugin_name", "http"),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "type", "HttpSensor"),
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "probe_id"),
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "update_date_time"),
				),
			},
			// Step 2: Import by ID — verify all state attributes round-trip correctly.
			{
				ResourceName:      "monotaur_sensor.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update every settable attribute. Assert all changes land in state.
			{
				Config: testAccSensorConfig(
					labelName, componentName, monitorName,
					updatedSensorName, "tcp", "TcpSensor", "{}",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_sensor.test", "name", updatedSensorName),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "plugin_name", "tcp"),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "type", "TcpSensor"),
					resource.TestCheckResourceAttr("monotaur_sensor.test", "parameters", "{}"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurSensor_drift: out-of-band change detection
//
// Steps:
//  1. Create the hierarchy and sensor. Capture the sensor ID.
//  2. PATCH name out-of-band via raw API (simulating an operator change outside
//     Terraform). Refresh state and assert that Terraform detects a non-empty
//     plan — the provider's Read must surface the mutated attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurSensor_drift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "sensor-drift")
	componentName := acctest.Name("component", "sensor-drift")
	monitorName := acctest.Name("monitor", "sensor-drift")
	sensorName := acctest.Name("sensor", "drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the sensor and capture its API-assigned ID.
			{
				Config: testAccSensorConfig(
					labelName, componentName, monitorName,
					sensorName, "http", "HttpSensor", "",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_sensor.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_sensor.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_sensor.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate name out-of-band, then refresh state and assert
			// that Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := sensorOutOfBandPatch(capturedID, acctest.Name("sensor", "drift-oob")); err != nil {
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
// TestAccMonotaurSensor_probeChange: changing probe_id triggers in-place update
//
// The probe_id attribute has no RequiresReplace plan modifier — changing it
// triggers an in-place update (PATCH) rather than destroy + create. This test
// asserts that the framework plans an Update action for the sensor when
// probe_id changes from probe1 to probe2.
//
// Steps:
//  1. Create label → component → monitor → probe1 + probe2 → sensor attached
//     to probe1. Assert probe_id == probe1.id.
//  2. Reconfigure to use probe2. Assert that Terraform plans an Update
//     (in-place) for the sensor, not a destroy-before-create. Assert
//     probe_id == probe2.id after apply.
// ---------------------------------------------------------------------------

func TestAccMonotaurSensor_probeChange(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	labelName := acctest.Name("label", "sensor-probechg")
	componentName := acctest.Name("component", "sensor-probechg")
	monitorName := acctest.Name("monitor", "sensor-probechg")
	sensorName := acctest.Name("sensor", "probechg")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create sensor attached to probe1. Assert probe_id is probe1.
			{
				Config: testAccSensorTwoProbesConfig(
					labelName, componentName, monitorName,
					sensorName, "probe1",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_sensor.test", "id"),
					resource.TestCheckResourceAttrPair(
						"monotaur_sensor.test", "probe_id",
						"monotaur_probe.probe1", "id",
					),
				),
			},
			// Step 2: Reconfigure to probe2. Because probe_id has no RequiresReplace,
			// the framework should plan an in-place Update.
			{
				Config: testAccSensorTwoProbesConfig(
					labelName, componentName, monitorName,
					sensorName, "probe2",
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"monotaur_sensor.test",
							plancheck.ResourceActionUpdate,
						),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"monotaur_sensor.test", "probe_id",
						"monotaur_probe.probe2", "id",
					),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccSensorConfig returns a Terraform configuration that creates the full
// hierarchy: label → component → monitor → probe → sensor. When parameters is
// empty it is omitted from the HCL so the API assigns defaults.
func testAccSensorConfig(labelName, componentName, monitorName, sensorName, pluginName, sensorType, parameters string) string {
	parametersBlock := ""
	if parameters != "" {
		parametersBlock = fmt.Sprintf("\n  parameters  = %q", parameters)
	}

	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}

resource "monotaur_component" "test" {
  name      = %q
  label_ids = [monotaur_label.test.id]
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_probe" "test" {
  monitor_id = monotaur_monitor.test.id
}

resource "monotaur_sensor" "test" {
  name        = %q
  plugin_name = %q
  type        = %q
  probe_id    = monotaur_probe.test.id%s
}
`, labelName, componentName, monitorName, sensorName, pluginName, sensorType, parametersBlock)
}

// testAccSensorTwoProbesConfig returns a Terraform configuration that creates
// two probes under the same monitor, allowing the sensor's probe_id to be
// re-pointed between them. activeProbe must be either "probe1" or "probe2" —
// it names the Terraform resource whose id is used as probe_id.
func testAccSensorTwoProbesConfig(labelName, componentName, monitorName, sensorName, activeProbe string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text = %q
}

resource "monotaur_component" "test" {
  name      = %q
  label_ids = [monotaur_label.test.id]
}

resource "monotaur_monitor" "test" {
  name          = %q
  component_ids = [monotaur_component.test.id]
}

resource "monotaur_probe" "probe1" {
  monitor_id = monotaur_monitor.test.id
}

resource "monotaur_probe" "probe2" {
  monitor_id = monotaur_monitor.test.id
}

resource "monotaur_sensor" "test" {
  name        = %q
  plugin_name = "http"
  type        = "HttpSensor"
  probe_id    = monotaur_probe.%s.id
}
`, labelName, componentName, monitorName, sensorName, activeProbe)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// sensorOutOfBandPatch performs a raw JSON:API PATCH on /sensors/{id} to
// change only the name field, simulating an operator change outside Terraform.
// The provider does not see the change until the next Read/Refresh.
func sensorOutOfBandPatch(id, newName string) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "sensors",
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

	url := fmt.Sprintf("%s/sensors/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /sensors/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

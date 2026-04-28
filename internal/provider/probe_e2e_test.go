package provider_test

// probe_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_probe resource. Tests are guarded by TF_ACC=1 and require
// MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurProbe_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update (active + schedule) → delete
//   - Drift detection: out-of-band PATCH on active attribute, plan detects non-empty diff
//   - sensor_ids cycling: start with 2 sensors, remove 1, add it back
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
// TestAccMonotaurProbe_basic: full lifecycle
//
// Steps:
//  1. Create label → component → monitor → probe (no sensor_ids). Assert id,
//     monitor_id, create_date_time, and update_date_time are set in state.
//  2. Import by ID. Assert round-trip equality (ImportStateVerify).
//  3. Update active to false and schedule to "*/10 * * * *". Assert both
//     changed attributes are reflected in state.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurProbe_basic(t *testing.T) {
	labelName := acctest.Name("label", "pb-basic")
	componentName := acctest.Name("component", "pb-basic")
	monitorName := acctest.Name("monitor", "pb-basic")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the full hierarchy and the probe. Assert all expected
			// attributes land in state.
			{
				Config: testAccProbeConfigBasic(labelName, componentName, monitorName, true, "*/5 * * * *"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "id"),
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "monitor_id"),
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "create_date_time"),
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "update_date_time"),
					resource.TestCheckResourceAttr("monotaur_probe.test", "active", "true"),
					resource.TestCheckResourceAttr("monotaur_probe.test", "schedule", "*/5 * * * *"),
				),
			},
			// Step 2: Import by ID — verify all state attributes round-trip correctly.
			{
				ResourceName:      "monotaur_probe.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: Update active to false and schedule to a new expression. Assert
			// both changes are reflected in state.
			{
				Config: testAccProbeConfigBasic(labelName, componentName, monitorName, false, "*/10 * * * *"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_probe.test", "active", "false"),
					resource.TestCheckResourceAttr("monotaur_probe.test", "schedule", "*/10 * * * *"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurProbe_drift: out-of-band change detection
//
// Steps:
//  1. Create the hierarchy and probe with active = true. Capture the probe ID.
//  2. PATCH active = false out-of-band via raw API (simulating an operator
//     change outside Terraform). Refresh state and assert that Terraform
//     detects a non-empty plan — the provider's Read must surface the mutated
//     attribute.
// ---------------------------------------------------------------------------

func TestAccMonotaurProbe_drift(t *testing.T) {
	labelName := acctest.Name("label", "pb-drift")
	componentName := acctest.Name("component", "pb-drift")
	monitorName := acctest.Name("monitor", "pb-drift")

	// capturedID is populated by the Check in step 1 and consumed in the
	// PreConfig of step 2. The framework calls Check and PreConfig sequentially,
	// never concurrently within a single test run.
	var capturedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create the probe with active = true and capture its API-assigned ID.
			{
				Config: testAccProbeConfigBasic(labelName, componentName, monitorName, true, "*/5 * * * *"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "id"),
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources["monotaur_probe.test"]
						if !ok {
							return fmt.Errorf("resource monotaur_probe.test not found in state")
						}
						capturedID = rs.Primary.ID
						if capturedID == "" {
							return fmt.Errorf("monotaur_probe.test ID is empty")
						}
						return nil
					},
				),
			},
			// Step 2: Mutate active out-of-band, then refresh state and assert that
			// Terraform plans a non-empty diff to restore the declared config.
			{
				PreConfig: func() {
					if capturedID == "" {
						t.Fatalf("drift test: capturedID is empty — create step must have failed")
					}
					if err := probeOutOfBandPatch(capturedID, false); err != nil {
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
// TestAccMonotaurProbe_sensorIDs: sensor_ids cycling
//
// This test validates that sensor_ids can be managed on the probe without
// creating a circular dependency. Sensors are always created with
// probe_id pointing at the probe; sensor_ids on the probe is used only as
// an explicit override in steps 2 and 3.
//
// Steps:
//  1. Config A: probe without sensor_ids + sensor_s1 + sensor_s2 (both with
//     probe_id = probe.id). After apply, the API populates probe.sensor_ids
//     automatically. Assert sensor_ids.# = "2".
//  2. Config B: probe with sensor_ids = [s1.id] + both sensors. Assert
//     sensor_ids.# = "1" and sensor_ids.0 matches s1.id.
//  3. Config C: probe with sensor_ids = [s1.id, s2.id] + both sensors. Assert
//     sensor_ids.# = "2".
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurProbe_sensorIDs(t *testing.T) {
	labelName := acctest.Name("label", "pb-sid")
	componentName := acctest.Name("component", "pb-sid")
	monitorName := acctest.Name("monitor", "pb-sid")
	sensor1Name := acctest.Name("sensor", "p1a")
	sensor2Name := acctest.Name("sensor", "p1b")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Config A — probe without sensor_ids, two sensors pointing at
			// the probe via probe_id. After apply + implicit read, the API returns
			// both sensors in probe.sensor_ids.
			{
				Config: testAccProbeConfigWithSensors(labelName, componentName, monitorName, sensor1Name, sensor2Name, false, nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("monotaur_probe.test", "id"),
					resource.TestCheckResourceAttr("monotaur_probe.test", "sensor_ids.#", "2"),
				),
			},
			// Step 2: Config B — probe with sensor_ids = [s1.id] only. The PATCH
			// removes s2 from the probe's sensor list.
			{
				Config: testAccProbeConfigWithSensors(labelName, componentName, monitorName, sensor1Name, sensor2Name, true, []string{"s1"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_probe.test", "sensor_ids.#", "1"),
					resource.TestCheckTypeSetElemAttrPair(
						"monotaur_probe.test", "sensor_ids.*",
						"monotaur_sensor.s1", "id",
					),
				),
			},
			// Step 3: Config C — probe with sensor_ids = [s1.id, s2.id]. The PATCH
			// adds s2 back to the probe's sensor list.
			{
				Config: testAccProbeConfigWithSensors(labelName, componentName, monitorName, sensor1Name, sensor2Name, true, []string{"s1", "s2"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_probe.test", "sensor_ids.#", "2"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccProbeConfigBasic returns a Terraform configuration that creates the
// full hierarchy: label → component → monitor → probe.
// active and schedule are set on the probe as explicit attributes.
func testAccProbeConfigBasic(labelName, componentName, monitorName string, active bool, schedule string) string {
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
  active     = %t
  schedule   = %q
}
`, labelName, componentName, monitorName, active, schedule)
}

// testAccProbeConfigWithSensors returns a Terraform configuration with the
// full hierarchy plus two sensors. When setSensorIDs is true, sensor_ids is
// set explicitly on the probe using the given sensorKeys (each element must be
// "s1" or "s2", naming the sensor resource). When setSensorIDs is false,
// sensor_ids is omitted so the API manages it via the sensors' probe_id links.
func testAccProbeConfigWithSensors(labelName, componentName, monitorName, sensor1Name, sensor2Name string, setSensorIDs bool, sensorKeys []string) string {
	sensorIDsBlock := ""
	if setSensorIDs && len(sensorKeys) > 0 {
		refs := make([]string, len(sensorKeys))
		for i, k := range sensorKeys {
			refs[i] = fmt.Sprintf("monotaur_sensor.%s.id", k)
		}
		sensorIDsBlock = fmt.Sprintf("\n  sensor_ids = [%s]", strings.Join(refs, ", "))
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
  monitor_id = monotaur_monitor.test.id%s
}

resource "monotaur_sensor" "s1" {
  name        = %q
  plugin_name = "http"
  type        = "HttpSensor"
  probe_id    = monotaur_probe.test.id
}

resource "monotaur_sensor" "s2" {
  name        = %q
  plugin_name = "http"
  type        = "HttpSensor"
  probe_id    = monotaur_probe.test.id
}
`, labelName, componentName, monitorName, sensorIDsBlock, sensor1Name, sensor2Name)
}

// ---------------------------------------------------------------------------
// Out-of-band mutation helper
// ---------------------------------------------------------------------------

// probeOutOfBandPatch performs a raw JSON:API PATCH on /probes/{id} to change
// only the active attribute, simulating an operator change outside Terraform.
// The provider does not see the change until the next Read/Refresh.
func probeOutOfBandPatch(id string, active bool) error {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "probes",
			"id":   id,
			"attributes": map[string]interface{}{
				"active": active,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch body: %w", err)
	}

	url := fmt.Sprintf("%s/probes/%s", endpoint, id)
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
		return fmt.Errorf("PATCH /probes/%s: HTTP %d", id, resp.StatusCode)
	}
	return nil
}

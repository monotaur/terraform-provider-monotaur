package provider_test

// alarm_note_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_alarm_note resource and data source. Tests are guarded by TF_ACC=1
// and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Covered acceptance criteria:
//   - Full lifecycle: create (public defaulted by the server) → import verify →
//     in-place update of kind, body and public → plan-only (no diff) →
//     moving the note to another alarm replaces it → destroy (verified
//     against the API)
//   - author is set by the server and kept across edits
//   - The data source reads the same values, including alarm_id

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestAccMonotaurAlarmNote_basic(t *testing.T) {
	monitorName := acctest.Name("monitor", "alarm-note")
	const addr = "monotaur_alarm_note.test"
	const ds = "data.monotaur_alarm_note.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckAPIResourcesGone("monotaur_alarm_note", "alarmNotes"),
		Steps: []resource.TestStep{
			// Step 1: create a note without public; the server defaults it to false.
			{
				Config: testAccAlarmNoteConfig(monitorName, "first", "Investigating", "Looking into **elevated** errors.", nil),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttrPair(addr, "alarm_id", "monotaur_alarm.first", "id"),
					resource.TestCheckResourceAttr(addr, "kind", "Investigating"),
					resource.TestCheckResourceAttr(addr, "body", "Looking into **elevated** errors."),
					resource.TestCheckResourceAttr(addr, "public", "false"),
					resource.TestMatchResourceAttr(addr, "author", alarmNoteAuthorPattern),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttrPair(ds, "alarm_id", addr, "alarm_id"),
					resource.TestCheckResourceAttrPair(ds, "author", addr, "author"),
					resource.TestCheckResourceAttr(ds, "kind", "Investigating"),
				),
			},
			// Step 2: import by ID and verify every attribute round-trips
			// (alarm_id comes from ?include=alarm).
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: edit kind, body and public in place; author is kept.
			{
				Config: testAccAlarmNoteConfig(monitorName, "first", "Resolved", "Rolled back.", alarmNoteBool(true)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "kind", "Resolved"),
					resource.TestCheckResourceAttr(addr, "body", "Rolled back."),
					resource.TestCheckResourceAttr(addr, "public", "true"),
					resource.TestMatchResourceAttr(addr, "author", alarmNoteAuthorPattern),
					resource.TestCheckResourceAttr(ds, "public", "true"),
				),
			},
			// Step 4: the same configuration plans no changes.
			{
				Config:   testAccAlarmNoteConfig(monitorName, "first", "Resolved", "Rolled back.", alarmNoteBool(true)),
				PlanOnly: true,
			},
			// Step 5: set public back to false explicitly (an Optional+Computed
			// bool must be sendable as false).
			{
				Config: testAccAlarmNoteConfig(monitorName, "first", "Resolved", "Rolled back.", alarmNoteBool(false)),
				Check:  resource.TestCheckResourceAttr(addr, "public", "false"),
			},
			// Step 6: pointing the note at another alarm replaces it, since the
			// API never moves a note between alarms.
			{
				Config: testAccAlarmNoteConfig(monitorName, "second", "Resolved", "Rolled back.", alarmNoteBool(false)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.TestCheckResourceAttrPair(addr, "alarm_id", "monotaur_alarm.second", "id"),
			},
		},
	})
}

// testAccAlarmNoteConfig returns a configuration with two monitors, an alarm
// on each, and one note on the alarm named by alarm ("first" or "second"), plus a
// data source reading the note. A nil public omits the attribute.
func testAccAlarmNoteConfig(monitorName, alarm, kind, body string, public *bool) string {
	pub := ""
	if public != nil {
		pub = fmt.Sprintf("\n  public   = %t", *public)
	}
	return fmt.Sprintf(`
resource "monotaur_monitor" "first" {
  name = "%[1]s-1"
}

resource "monotaur_monitor" "second" {
  name = "%[1]s-2"
}

# A monitor has at most one open alarm, so each alarm gets its own monitor.
resource "monotaur_alarm" "first" {
  monitor_id = monotaur_monitor.first.id
}

resource "monotaur_alarm" "second" {
  monitor_id = monotaur_monitor.second.id
}

resource "monotaur_alarm_note" "test" {
  alarm_id = monotaur_alarm.%[2]s.id
  kind     = %[3]q
  body     = %[4]q%[5]s
}

data "monotaur_alarm_note" "test" {
  id = monotaur_alarm_note.test.id
}
`, monitorName, strings.TrimSpace(alarm), kind, body, pub)
}

// alarmNoteAuthorPattern matches the author the server records for an API
// key's service account.
var alarmNoteAuthorPattern = regexp.MustCompile(`^service-account:\d+$`)

func alarmNoteBool(v bool) *bool { return &v }

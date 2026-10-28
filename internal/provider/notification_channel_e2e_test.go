package provider_test

// notification_channel_e2e_test.go contains end-to-end acceptance tests for
// the monotaur_notification_channel resource. Tests are guarded by TF_ACC=1
// and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Test naming convention: TestAccMonotaurNotificationChannel_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update → no-op plan → delete.
//   - credential is write-only (never returned), so it is ignored on import verify.
//   - Clearing body_template (removing it from config) sends an explicit null and
//     restores the type's default template (state reads back null).
//   - All resource names use acctest.Name() for collision-safe, sweepable identifiers.

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ncTestBodyTemplate is a Webhook body template: a Webhook body must render JSON.
const ncTestBodyTemplate = `{"summary": {{ summary | object.to_json }}}`

// ---------------------------------------------------------------------------
// TestAccMonotaurNotificationChannel_basic: full lifecycle
//
// Steps:
//  1. Create a Webhook channel with a credential and a body template.
//  2. Import by ID; credential is ignored (the API never returns it).
//  3. Update name, event_types and enabled, and remove body_template (cleared
//     to null on the server).
//  4. Plan only with the same config: expect no changes.
//
// Deletion is performed automatically by the test framework after all steps.
// ---------------------------------------------------------------------------

func TestAccMonotaurNotificationChannel_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	name := acctest.Name("notification-channel", "1")
	updatedName := acctest.Name("notification-channel", "1-upd")
	const res = "monotaur_notification_channel.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create.
			{
				Config: testAccNotificationChannelConfigCreate(name),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "id"),
					resource.TestCheckResourceAttr(res, "name", name),
					resource.TestCheckResourceAttr(res, "type", "Webhook"),
					resource.TestCheckResourceAttr(res, "event_types.#", "1"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.triggered"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "credential", "https://example.com/hook"),
					resource.TestCheckResourceAttr(res, "recipients.#", "0"),
					resource.TestCheckResourceAttr(res, "body_template", ncTestBodyTemplate),
					resource.TestCheckNoResourceAttr(res, "subject_template"),
					resource.TestCheckResourceAttr(res, "consecutive_failure_count", "0"),
					resource.TestCheckNoResourceAttr(res, "auto_disabled_date_time"),
					resource.TestCheckResourceAttrSet(res, "create_date_time"),
					resource.TestCheckResourceAttrSet(res, "update_date_time"),
				),
			},
			// Step 2: Import. credential is write-only and cannot be imported.
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"credential"},
			},
			// Step 3: Update in place; body_template removed → cleared to null.
			{
				Config: testAccNotificationChannelConfigUpdate(updatedName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(res, "name", updatedName),
					resource.TestCheckResourceAttr(res, "event_types.#", "2"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.triggered"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.cleared"),
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckNoResourceAttr(res, "body_template"),
					resource.TestCheckResourceAttr(res, "credential", "https://example.com/hook"),
				),
			},
			// Step 4: Plan only — no drift.
			{
				Config:   testAccNotificationChannelConfigUpdate(updatedName),
				PlanOnly: true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func testAccNotificationChannelConfigCreate(name string) string {
	return fmt.Sprintf(`
resource "monotaur_notification_channel" "test" {
  name          = %q
  type          = "Webhook"
  event_types   = ["com.monotaur.alarm.triggered"]
  credential    = "https://example.com/hook"
  body_template = %q
}
`, name, ncTestBodyTemplate)
}

func testAccNotificationChannelConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "monotaur_notification_channel" "test" {
  name        = %q
  type        = "Webhook"
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
  enabled     = false
  credential  = "https://example.com/hook"
}
`, name)
}

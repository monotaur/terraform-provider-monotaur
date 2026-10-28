package provider_test

// webhook_subscription_e2e_test.go contains end-to-end acceptance tests for
// the monotaur_webhook_subscription resource and data source. Tests are
// guarded by TF_ACC=1 and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY.
//
// Test naming convention: TestAccMonotaurWebhookSubscription_<scenario>
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import → update → no-op plan → delete.
//   - signing_key is write-only (never returned), so it is ignored on import verify.
//   - Removing Optional+Computed attributes from the configuration keeps their
//     server values (empty plan); removing signing_key keeps the server's key.
//   - A subscription created without signing_key exposes the server-generated
//     key once as generated_signing_key; setting signing_key later clears it.
//   - An out-of-band rotate-signing-key shows up as
//     previous_signing_key_expiry_date_time without a diff; changing
//     signing_key ends the overlap (clears it).
//   - Subscriptions have no name, so each URL carries an acctest.Name() path
//     segment, which the sweeper matches.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// Signing keys used by the tests: at least 32 bytes of UTF-8.
const (
	wsE2EKey1 = "tfe2e-signing-key-one-0123456789abcdef"
	wsE2EKey2 = "tfe2e-signing-key-two-0123456789abcdef"
)

// wsE2EURL returns a webhook URL carrying an acctest name, so the sweeper
// recognises it. example.com resolves to a public address the egress policy allows.
func wsE2EURL(n string) string {
	return "https://example.com/" + acctest.Name("webhook-subscription", n) + "/hook"
}

// ---------------------------------------------------------------------------
// TestAccMonotaurWebhookSubscription_basic: full lifecycle
//
// Steps:
//  1. Create with every optional attribute set, including signing_key.
//  2. Import by ID; signing_key/generated_signing_key are ignored (never returned).
//  3. Update url, event_types, payload_mode, enabled, legacy_signature_enabled
//     and signing_key in place.
//  4. Plan only with payload_mode, enabled and legacy_signature_enabled removed
//     from the configuration: Optional+Computed, so no changes.
//  5. Remove signing_key from the configuration: state drops it, server keeps it.
//  6. Plan only: no drift.
// ---------------------------------------------------------------------------

func TestAccMonotaurWebhookSubscription_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	url := wsE2EURL("1")
	updatedURL := wsE2EURL("1-upd")
	const res = "monotaur_webhook_subscription.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create.
			{
				Config: fmt.Sprintf(`
resource "monotaur_webhook_subscription" "test" {
  url                      = %q
  event_types              = ["com.monotaur.alarm.triggered"]
  payload_mode             = "ChangesOnly"
  enabled                  = true
  legacy_signature_enabled = true
  signing_key              = %q
}
`, url, wsE2EKey1),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "id"),
					resource.TestCheckResourceAttr(res, "url", url),
					resource.TestCheckResourceAttr(res, "event_types.#", "1"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.triggered"),
					resource.TestCheckResourceAttr(res, "payload_mode", "ChangesOnly"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "legacy_signature_enabled", "true"),
					resource.TestCheckResourceAttr(res, "signing_key", wsE2EKey1),
					resource.TestCheckNoResourceAttr(res, "generated_signing_key"),
					resource.TestCheckNoResourceAttr(res, "previous_signing_key_expiry_date_time"),
					resource.TestCheckResourceAttr(res, "consecutive_failure_count", "0"),
					resource.TestCheckNoResourceAttr(res, "auto_disabled_date_time"),
					resource.TestCheckResourceAttrSet(res, "create_date_time"),
					resource.TestCheckResourceAttrSet(res, "update_date_time"),
				),
			},
			// Step 2: Import. The signing keys are write-only and cannot be imported.
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_key", "generated_signing_key"},
			},
			// Step 3: Update everything in place, including the signing key.
			{
				Config: testAccWebhookSubscriptionConfigUpdated(updatedURL, `
  payload_mode             = "Full"
  enabled                  = false
  legacy_signature_enabled = false
  signing_key              = "`+wsE2EKey2+`"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(res, "url", updatedURL),
					resource.TestCheckResourceAttr(res, "event_types.#", "2"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.triggered"),
					resource.TestCheckTypeSetElemAttr(res, "event_types.*", "com.monotaur.alarm.acknowledged"),
					resource.TestCheckResourceAttr(res, "payload_mode", "Full"),
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckResourceAttr(res, "legacy_signature_enabled", "false"),
					resource.TestCheckResourceAttr(res, "signing_key", wsE2EKey2),
					resource.TestCheckNoResourceAttr(res, "generated_signing_key"),
					resource.TestCheckNoResourceAttr(res, "previous_signing_key_expiry_date_time"),
				),
			},
			// Step 4: Optional+Computed attributes removed → server values kept, no changes.
			{
				Config: testAccWebhookSubscriptionConfigUpdated(updatedURL, `
  signing_key = "`+wsE2EKey2+`"`),
				PlanOnly: true,
			},
			// Step 5: signing_key removed → dropped from state; the key on the server is unchanged.
			{
				Config: testAccWebhookSubscriptionConfigUpdated(updatedURL, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr(res, "signing_key"),
					resource.TestCheckNoResourceAttr(res, "generated_signing_key"),
					resource.TestCheckResourceAttr(res, "payload_mode", "Full"),
					resource.TestCheckResourceAttr(res, "enabled", "false"),
					resource.TestCheckResourceAttr(res, "legacy_signature_enabled", "false"),
				),
			},
			// Step 6: Plan only — no drift.
			{
				Config:   testAccWebhookSubscriptionConfigUpdated(updatedURL, ""),
				PlanOnly: true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurWebhookSubscription_generatedKey: server defaults, generated
// key, out-of-band rotation and the data source
//
// Steps:
//  1. Create with only url and event_types: server defaults apply and the
//     generated key is exposed as generated_signing_key; the data source reads it.
//  2. Import by ID.
//  3. Rotate the key outside Terraform: previous_signing_key_expiry_date_time
//     is set, with no diff.
//  4. Set signing_key: generated_signing_key and the rotation overlap are cleared.
//  5. Plan only — no drift.
// ---------------------------------------------------------------------------

func TestAccMonotaurWebhookSubscription_generatedKey(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	url := wsE2EURL("2")
	const res = "monotaur_webhook_subscription.test"
	const ds = "data.monotaur_webhook_subscription.test"
	var id string

	configNoKey := fmt.Sprintf(`
resource "monotaur_webhook_subscription" "test" {
  url         = %q
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
}

data "monotaur_webhook_subscription" "test" {
  id = monotaur_webhook_subscription.test.id
}
`, url)
	configKey := fmt.Sprintf(`
resource "monotaur_webhook_subscription" "test" {
  url         = %q
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
  signing_key = %q
}
`, url, wsE2EKey1)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create with server defaults and a generated key.
			{
				Config: configNoKey,
				Check: resource.ComposeTestCheckFunc(
					func(s *terraform.State) error {
						rs, ok := s.RootModule().Resources[res]
						if !ok {
							return fmt.Errorf("%s not in state", res)
						}
						id = rs.Primary.ID
						return nil
					},
					resource.TestCheckResourceAttr(res, "payload_mode", "Full"),
					resource.TestCheckResourceAttr(res, "enabled", "true"),
					resource.TestCheckResourceAttr(res, "legacy_signature_enabled", "false"),
					resource.TestCheckNoResourceAttr(res, "signing_key"),
					resource.TestCheckResourceAttrWith(res, "generated_signing_key", func(v string) error {
						if len(v) < 32 {
							return fmt.Errorf("generated_signing_key: want at least 32 bytes, got %d", len(v))
						}
						return nil
					}),
					resource.TestCheckNoResourceAttr(res, "previous_signing_key_expiry_date_time"),
					resource.TestCheckResourceAttrPair(ds, "id", res, "id"),
					resource.TestCheckResourceAttrPair(ds, "url", res, "url"),
					resource.TestCheckResourceAttr(ds, "event_types.#", "2"),
					resource.TestCheckTypeSetElemAttr(ds, "event_types.*", "com.monotaur.alarm.cleared"),
					resource.TestCheckResourceAttrPair(ds, "payload_mode", res, "payload_mode"),
					resource.TestCheckResourceAttrPair(ds, "enabled", res, "enabled"),
					resource.TestCheckResourceAttrPair(ds, "legacy_signature_enabled", res, "legacy_signature_enabled"),
					resource.TestCheckResourceAttrPair(ds, "create_date_time", res, "create_date_time"),
					resource.TestCheckResourceAttr(ds, "consecutive_failure_count", "0"),
					resource.TestCheckNoResourceAttr(ds, "signing_key"),
				),
			},
			// Step 2: Import.
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_key", "generated_signing_key"},
			},
			// Step 3: Rotate outside Terraform; refresh picks up the overlap, no diff.
			{
				PreConfig: func() {
					c, err := newSweepClient()
					if err != nil {
						t.Fatalf("sweep client: %v", err)
					}
					if err := c.post(context.Background(), "/webhookSubscriptions/"+id+"/rotate-signing-key"); err != nil {
						t.Fatalf("rotate-signing-key: %v", err)
					}
				},
				Config: configNoKey,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "previous_signing_key_expiry_date_time"),
					resource.TestCheckResourceAttrSet(res, "generated_signing_key"),
				),
			},
			// Step 4: Set signing_key → generated key and rotation overlap cleared.
			{
				Config: configKey,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(res, "signing_key", wsE2EKey1),
					resource.TestCheckNoResourceAttr(res, "generated_signing_key"),
					resource.TestCheckNoResourceAttr(res, "previous_signing_key_expiry_date_time"),
				),
			},
			// Step 5: Plan only — no drift.
			{
				Config:   configKey,
				PlanOnly: true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccWebhookSubscriptionConfigUpdated is the updated subscription with
// extra attribute lines appended.
func testAccWebhookSubscriptionConfigUpdated(url, extra string) string {
	return fmt.Sprintf(`
resource "monotaur_webhook_subscription" "test" {
  url         = %q
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.acknowledged"]%s
}
`, url, extra)
}

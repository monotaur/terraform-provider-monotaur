package provider_test

// notification_policy_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_notification_policy and monotaur_notification_escalation_step
// resources. Tests are guarded by TF_ACC=1 and require MONOTAUR_ENDPOINT and
// MONOTAUR_API_KEY to be set. They depend on monotaur_notification_channel
// for the channels the policy and step target.
//
// Covered acceptance criteria:
//   - Policy lifecycle: create (all attributes, channel + label) → import →
//     update (rename, disable, clear minimum_severity / repeat_interval_minutes
//     via explicit nulls, clear label_ids via `data: []`) → destroy
//   - Step lifecycle: create → import → update (delay, add a channel) → destroy
//   - All resource names use acctest.Name()/acctest.LabelText() so they are sweepable.

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurNotificationPolicy_basic
// ---------------------------------------------------------------------------

func TestAccMonotaurNotificationPolicy_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	channelName := acctest.Name("notification-channel", "pol1")
	labelText := acctest.LabelText("pol1")
	policyName := acctest.Name("notification-policy", "1")
	updatedName := acctest.Name("notification-policy", "1-upd")

	const addr = "monotaur_notification_policy.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: create with every attribute and a channel + label.
			{
				Config: testAccNotificationPolicyConfigFull(channelName, labelText, policyName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "name", policyName),
					resource.TestCheckResourceAttr(addr, "enabled", "true"),
					resource.TestCheckResourceAttr(addr, "minimum_severity", "Error"),
					resource.TestCheckResourceAttr(addr, "repeat_interval_minutes", "15"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttr(addr, "channel_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "channel_ids.0", "monotaur_notification_channel.c1", "id"),
					resource.TestCheckResourceAttr(addr, "label_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "label_ids.0", "monotaur_label.l1", "id"),
					resource.TestCheckResourceAttr(addr, "component_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "monitor_ids.#", "0"),
				),
			},
			// Step 2: import by ID. Read requests the relationships via
			// `include`, so the ID lists round-trip too.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: rename, disable, drop the nullable attributes (sent as
			// explicit nulls) and clear label_ids (sent as `data: []`). The label
			// stays in config so it is not deleted while the policy names it.
			{
				Config: testAccNotificationPolicyConfigCleared(channelName, labelText, updatedName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", updatedName),
					resource.TestCheckResourceAttr(addr, "enabled", "false"),
					resource.TestCheckNoResourceAttr(addr, "minimum_severity"),
					resource.TestCheckNoResourceAttr(addr, "repeat_interval_minutes"),
					resource.TestCheckResourceAttr(addr, "label_ids.#", "0"),
					resource.TestCheckResourceAttr(addr, "channel_ids.#", "1"),
				),
			},
			// Step 4: import again to confirm the cleared values are what the API holds.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurNotificationEscalationStep_basic
// ---------------------------------------------------------------------------

func TestAccMonotaurNotificationEscalationStep_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	channel1Name := acctest.Name("notification-channel", "step1a")
	channel2Name := acctest.Name("notification-channel", "step1b")
	policyName := acctest.Name("notification-policy", "step1")

	const addr = "monotaur_notification_escalation_step.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: an escalation-only policy (no channels of its own) with one step.
			{
				Config: testAccNotificationEscalationStepConfig(channel1Name, channel2Name, policyName, 30,
					"monotaur_notification_channel.c1.id"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttrPair(addr, "notification_policy_id", "monotaur_notification_policy.test", "id"),
					resource.TestCheckResourceAttr(addr, "delay_minutes", "30"),
					resource.TestCheckResourceAttr(addr, "channel_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "channel_ids.0", "monotaur_notification_channel.c1", "id"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					// Adding a step must not show up as a diff on the policy.
					resource.TestCheckResourceAttr("monotaur_notification_policy.test", "channel_ids.#", "0"),
				),
			},
			// Step 2: import by ID; notification_policy_id and channel_ids
			// come back through `include`.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: change the delay and add the second channel (in place).
			{
				Config: testAccNotificationEscalationStepConfig(channel1Name, channel2Name, policyName, 45,
					"monotaur_notification_channel.c1.id", "monotaur_notification_channel.c2.id"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "delay_minutes", "45"),
					resource.TestCheckResourceAttr(addr, "channel_ids.#", "2"),
					resource.TestCheckResourceAttrPair(addr, "channel_ids.0", "monotaur_notification_channel.c1", "id"),
					resource.TestCheckResourceAttrPair(addr, "channel_ids.1", "monotaur_notification_channel.c2", "id"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccNotificationChannelBlock returns a webhook channel subscribed to
// alarm.triggered (the event type repeats and escalations are sent as).
func testAccNotificationChannelBlock(label, name string) string {
	return fmt.Sprintf(`
resource "monotaur_notification_channel" %q {
  name        = %q
  type        = "Webhook"
  event_types = ["com.monotaur.alarm.triggered"]
  credential  = "https://example.com/hook"
}
`, label, name)
}

func testAccNotificationPolicyConfigFull(channelName, labelText, policyName string) string {
	return testAccNotificationChannelBlock("c1", channelName) + fmt.Sprintf(`
resource "monotaur_label" "l1" {
  text = %q
}

resource "monotaur_notification_policy" "test" {
  name                    = %q
  minimum_severity        = "Error"
  repeat_interval_minutes = 15
  channel_ids             = [monotaur_notification_channel.c1.id]
  label_ids               = [monotaur_label.l1.id]
}
`, labelText, policyName)
}

func testAccNotificationPolicyConfigCleared(channelName, labelText, policyName string) string {
	return testAccNotificationChannelBlock("c1", channelName) + fmt.Sprintf(`
resource "monotaur_label" "l1" {
  text = %q
}

resource "monotaur_notification_policy" "test" {
  name        = %q
  enabled     = false
  channel_ids = [monotaur_notification_channel.c1.id]
  label_ids   = []
}
`, labelText, policyName)
}

// testAccNotificationEscalationStepConfig returns two channels, a policy with
// no channels of its own and one escalation step targeting channelRefs
// (HCL expressions, not quoted).
func testAccNotificationEscalationStepConfig(channel1Name, channel2Name, policyName string, delay int, channelRefs ...string) string {
	refs := ""
	for i, r := range channelRefs {
		if i > 0 {
			refs += ", "
		}
		refs += r
	}
	return testAccNotificationChannelBlock("c1", channel1Name) +
		testAccNotificationChannelBlock("c2", channel2Name) +
		fmt.Sprintf(`
resource "monotaur_notification_policy" "test" {
  name = %q
}

resource "monotaur_notification_escalation_step" "test" {
  notification_policy_id = monotaur_notification_policy.test.id
  delay_minutes          = %d
  channel_ids            = [%s]
}
`, policyName, delay, refs)
}

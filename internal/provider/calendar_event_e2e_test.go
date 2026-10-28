package provider_test

// calendar_event_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_calendar_event resource and data source. Tests are guarded by
// TF_ACC=1 and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY.
//
// Covered acceptance criteria:
//   - an end before the start is rejected at plan time
//   - create (every attribute, a label, UTC timestamps) → data source reads it back
//   - import by ID (label_ids round-trip through `include`)
//   - update: title, clear description / external_reference (explicit nulls),
//     switch labels, timestamps with a +02:00 offset and fractional seconds
//     (the API returns them in UTC; the configured spelling is kept)
//   - plan-only: re-planning the updated config is empty
//   - label_ids = [] clears the links
//   - updating a label that does not configure calendar_event_ids keeps the
//     event's link to it
//   - titles use acctest.Name() and label texts acctest.LabelText() so the
//     sweepers match them.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestAccMonotaurCalendarEvent_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	title := acctest.Name("calendar-event", "1")
	updatedTitle := acctest.Name("calendar-event", "1-upd")
	label1 := acctest.LabelText("ce1-a")
	label2 := acctest.LabelText("ce1-b")

	const addr = "monotaur_calendar_event.test"
	const dsAddr = "data.monotaur_calendar_event.test"

	updatedStart := "2031-06-01T22:00:00+02:00"
	updatedEnd := "2031-06-02T01:30:00.25+02:00"

	full := func(start, end string) string {
		return testAccCalendarEventLabels(label1, label2) + fmt.Sprintf(`
resource "monotaur_calendar_event" "test" {
  title              = %q
  description        = "Rolled out release 4.2"
  external_reference = "CHG-1234"
  start_date_time    = %q
  end_date_time      = %q
  label_ids          = [monotaur_label.l1.id]
}
`, title, start, end)
	}
	updated := func(labels string) string {
		return testAccCalendarEventLabels(label1, label2) + fmt.Sprintf(`
resource "monotaur_calendar_event" "test" {
  title           = %q
  start_date_time = %q
  end_date_time   = %q
  label_ids       = %s
}
`, updatedTitle, updatedStart, updatedEnd, labels)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: an end before the start fails validation before any API call.
			{
				Config:      full("2031-01-01T10:00:00Z", "2031-01-01T09:00:00Z"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must not be before start_date_time`),
			},
			// Step 2: create with every attribute and one label.
			{
				Config: full("2031-01-01T10:00:00Z", "2031-01-01T12:00:00Z") + `
data "monotaur_calendar_event" "test" {
  id = monotaur_calendar_event.test.id
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "title", title),
					resource.TestCheckResourceAttr(addr, "description", "Rolled out release 4.2"),
					resource.TestCheckResourceAttr(addr, "external_reference", "CHG-1234"),
					resource.TestCheckResourceAttr(addr, "start_date_time", "2031-01-01T10:00:00Z"),
					resource.TestCheckResourceAttr(addr, "end_date_time", "2031-01-01T12:00:00Z"),
					resource.TestCheckResourceAttr(addr, "label_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "label_ids.0", "monotaur_label.l1", "id"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttrPair(dsAddr, "id", addr, "id"),
					resource.TestCheckResourceAttr(dsAddr, "title", title),
					resource.TestCheckResourceAttr(dsAddr, "description", "Rolled out release 4.2"),
					resource.TestCheckResourceAttr(dsAddr, "external_reference", "CHG-1234"),
					resource.TestCheckResourceAttr(dsAddr, "start_date_time", "2031-01-01T10:00:00Z"),
					resource.TestCheckResourceAttr(dsAddr, "end_date_time", "2031-01-01T12:00:00Z"),
					resource.TestCheckResourceAttr(dsAddr, "label_ids.#", "1"),
					resource.TestCheckResourceAttrPair(dsAddr, "label_ids.0", "monotaur_label.l1", "id"),
				),
			},
			// Step 3: import by ID.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 4: rename, drop description and external_reference (sent as
			// explicit nulls), switch to the other label, and move the event
			// with offset timestamps that are kept as written.
			{
				Config: updated("[monotaur_label.l2.id]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", updatedTitle),
					resource.TestCheckNoResourceAttr(addr, "description"),
					resource.TestCheckNoResourceAttr(addr, "external_reference"),
					resource.TestCheckResourceAttr(addr, "start_date_time", updatedStart),
					resource.TestCheckResourceAttr(addr, "end_date_time", updatedEnd),
					resource.TestCheckResourceAttr(addr, "label_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "label_ids.0", "monotaur_label.l2", "id"),
					testAccCheckCalendarEventAPILabels(addr, "monotaur_label.l2"),
				),
			},
			// Step 5: re-planning the same config must be empty.
			{
				Config:   updated("[monotaur_label.l2.id]"),
				PlanOnly: true,
			},
			// Step 6: import again; the API holds the cleared values and the same
			// instants in UTC.
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"start_date_time", "end_date_time"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported instance, got %d", len(states))
					}
					want := map[string]string{
						"start_date_time": "2031-06-01T20:00:00Z",
						"end_date_time":   "2031-06-01T23:30:00.25Z",
					}
					for k, v := range want {
						if got := states[0].Attributes[k]; got != v {
							return fmt.Errorf("imported %s: want %q, got %q", k, v, got)
						}
					}
					return nil
				},
			},
			// Step 7: label_ids = [] removes every link.
			{
				Config: updated("[]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "label_ids.#", "0"),
					testAccCheckCalendarEventAPILabels(addr),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurCalendarEvent_keepsLinksOnLabelUpdate
//
// A label that does not configure calendar_event_ids must leave links made
// with monotaur_calendar_event.label_ids alone when it is updated.
//
// Steps:
//  1. Create a label and an event linked to it. Assert the API holds the link.
//  2. Change the label's colour (a label update). Assert the link is still
//     there; the post-apply plan being empty shows the event sees no drift.
// ---------------------------------------------------------------------------

func TestAccMonotaurCalendarEvent_keepsLinksOnLabelUpdate(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	title := acctest.Name("calendar-event", "lblink")
	labelText := acctest.LabelText("ce-lblink")

	cfg := func(color string) string {
		return fmt.Sprintf(`
resource "monotaur_label" "test" {
  text  = %q
  color = %q
}

resource "monotaur_calendar_event" "test" {
  title           = %q
  start_date_time = "2031-02-01T10:00:00Z"
  end_date_time   = "2031-02-01T10:00:00Z"
  label_ids       = [monotaur_label.test.id]
}
`, labelText, color, title)
	}

	const addr = "monotaur_calendar_event.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("#FF5733"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(addr, "label_ids.0", "monotaur_label.test", "id"),
					testAccCheckCalendarEventAPILabels(addr, "monotaur_label.test"),
				),
			},
			{
				Config: cfg("#00AA00"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_label.test", "color", "#00AA00"),
					resource.TestCheckResourceAttr(addr, "label_ids.#", "1"),
					resource.TestCheckResourceAttrPair(addr, "label_ids.0", "monotaur_label.test", "id"),
					testAccCheckCalendarEventAPILabels(addr, "monotaur_label.test"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testAccCalendarEventLabels(text1, text2 string) string {
	return fmt.Sprintf(`
resource "monotaur_label" "l1" {
  text = %q
}

resource "monotaur_label" "l2" {
  text = %q
}
`, text1, text2)
}

// testAccCheckCalendarEventAPILabels asserts, straight from the API, that the
// event at eventAddr is linked to exactly the labels at labelAddrs.
func testAccCheckCalendarEventAPILabels(eventAddr string, labelAddrs ...string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		eventID := s.RootModule().Resources[eventAddr].Primary.ID
		want := make(map[string]bool, len(labelAddrs))
		for _, a := range labelAddrs {
			want[s.RootModule().Resources[a].Primary.ID] = true
		}

		c, err := newSweepClient()
		if err != nil {
			return err
		}
		var doc struct {
			Data []resourceID `json:"data"`
		}
		if err := c.get(context.Background(), "/calendarEvents/"+eventID+"/relationships/labels?"+notificationSweepPageSize, &doc); err != nil {
			return fmt.Errorf("listing labels of calendar event %s: %w", eventID, err)
		}
		if len(doc.Data) != len(want) {
			return fmt.Errorf("calendar event %s: want %d label(s), API has %v", eventID, len(want), doc.Data)
		}
		for _, l := range doc.Data {
			if !want[l.ID] {
				return fmt.Errorf("calendar event %s: unexpected label %s (want %v)", eventID, l.ID, want)
			}
		}
		return nil
	}
}

package provider_test

// component_maintenance_e2e_test.go checks that managing a component does not
// break the links a maintenance schedule gives its generated windows.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurComponent_keepsScheduleWindowLinks
//
// A maintenance schedule links its generated window to the schedule's
// components on the server. A component that does not configure
// maintenance_window_ids must leave those links alone when it is updated.
//
// Steps:
//  1. Create a component and a schedule targeting it. Assert the schedule's
//     generated window is linked to the component.
//  2. Rename the component. Assert the link is still there.
// ---------------------------------------------------------------------------

func TestAccMonotaurComponent_keepsScheduleWindowLinks(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	componentName := acctest.Name("component", "mwlink")
	scheduleDescription := acctest.Name("maintenance-schedule", "mwlink")

	cfg := func(name string) string {
		return fmt.Sprintf(`
resource "monotaur_component" "test" {
  name = %q
}

resource "monotaur_maintenance_schedule" "test" {
  description   = %q
  schedule      = "0 3 * * 0"
  time_span     = "01:00:00"
  component_ids = [monotaur_component.test.id]
}
`, name, scheduleDescription)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg(componentName),
				Check:  testAccCheckScheduleWindowLinkedToComponent,
			},
			{
				Config: cfg(componentName + "-renamed"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("monotaur_component.test", "name", componentName+"-renamed"),
					testAccCheckScheduleWindowLinkedToComponent,
				),
			},
		},
	})
}

// testAccCheckScheduleWindowLinkedToComponent asserts that the schedule's
// generated window lists the component among its components.
func testAccCheckScheduleWindowLinkedToComponent(s *terraform.State) error {
	componentID := s.RootModule().Resources["monotaur_component.test"].Primary.ID
	scheduleID := s.RootModule().Resources["monotaur_maintenance_schedule.test"].Primary.ID

	c, err := newSweepClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	var windows collectionDoc
	if err := c.get(ctx, "/maintenanceSchedules/"+scheduleID+"/maintenanceWindows", &windows); err != nil {
		return fmt.Errorf("listing generated windows: %w", err)
	}
	if len(windows.Data) == 0 {
		return fmt.Errorf("schedule %s has no generated window", scheduleID)
	}
	for _, w := range windows.Data {
		var components collectionDoc
		if err := c.get(ctx, "/maintenanceWindows/"+w.ID+"/components", &components); err != nil {
			return fmt.Errorf("listing components of window %s: %w", w.ID, err)
		}
		for _, comp := range components.Data {
			if comp.ID == componentID {
				return nil
			}
		}
	}
	return fmt.Errorf("no generated window of schedule %s is linked to component %s", scheduleID, componentID)
}

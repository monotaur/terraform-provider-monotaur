package provider_test

// business_hours_e2e_test.go contains end-to-end acceptance tests for the
// monotaur_business_hours resource and data source. Tests are guarded by
// TF_ACC=1 and require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY to be set.
//
// Covered acceptance criteria:
//   - Full lifecycle: create → import verify → update (incl. clearing
//     time_zone_id) → plan-only (no diff) → overnight range with a Windows
//     time zone → plan-only → destroy (verified against the API)
//   - days_of_week round-trips although the API renders Monday–Friday as
//     "Weekdays" and all seven days as "Everyday"
//   - A non-normalised TimeSpan ("8:0:0") produces no diff after the API
//     returns "08:00:00"
//   - The data source reads the same values

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestAccMonotaurBusinessHours_basic(t *testing.T) {
	name := acctest.Name("businesshours", "basic")
	renamed := acctest.Name("businesshours", "basic-renamed")
	const addr = "monotaur_business_hours.test"

	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}
	everyday := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckAPIResourcesGone("monotaur_business_hours", "businessHours"),
		Steps: []resource.TestStep{
			// Step 1: create Monday–Friday (returned by the API as "Weekdays")
			// with a time zone, and read it back through the data source.
			{
				Config: testAccBusinessHoursConfig(name, weekdays, "09:00:00", "17:30:00", "Europe/Berlin"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "name", name),
					resource.TestCheckResourceAttr(addr, "days_of_week.#", "5"),
					resource.TestCheckTypeSetElemAttr(addr, "days_of_week.*", "Monday"),
					resource.TestCheckTypeSetElemAttr(addr, "days_of_week.*", "Friday"),
					resource.TestCheckResourceAttr(addr, "start_time", "09:00:00"),
					resource.TestCheckResourceAttr(addr, "end_time", "17:30:00"),
					resource.TestCheckResourceAttr(addr, "time_zone_id", "Europe/Berlin"),
					resource.TestCheckResourceAttrSet(addr, "create_date_time"),
					resource.TestCheckResourceAttrSet(addr, "update_date_time"),
					resource.TestCheckResourceAttrPair("data.monotaur_business_hours.test", "name", addr, "name"),
					resource.TestCheckResourceAttr("data.monotaur_business_hours.test", "days_of_week.#", "5"),
					resource.TestCheckResourceAttr("data.monotaur_business_hours.test", "start_time", "09:00:00"),
					resource.TestCheckResourceAttr("data.monotaur_business_hours.test", "time_zone_id", "Europe/Berlin"),
				),
			},
			// Step 2: import by ID and verify every attribute round-trips.
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Step 3: rename, change days and times (with a non-normalised
			// TimeSpan and a midnight end), and clear time_zone_id.
			{
				Config: testAccBusinessHoursConfig(renamed, []string{"Saturday", "Sunday", "Wednesday"}, "8:0:0", "1.00:00:00", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", renamed),
					resource.TestCheckResourceAttr(addr, "days_of_week.#", "3"),
					resource.TestCheckTypeSetElemAttr(addr, "days_of_week.*", "Saturday"),
					resource.TestCheckTypeSetElemAttr(addr, "days_of_week.*", "Sunday"),
					resource.TestCheckTypeSetElemAttr(addr, "days_of_week.*", "Wednesday"),
					resource.TestCheckResourceAttr(addr, "start_time", "8:0:0"),
					resource.TestCheckResourceAttr(addr, "end_time", "1.00:00:00"),
					resource.TestCheckNoResourceAttr(addr, "time_zone_id"),
					resource.TestCheckNoResourceAttr("data.monotaur_business_hours.test", "time_zone_id"),
					resource.TestCheckResourceAttr("data.monotaur_business_hours.test", "start_time", "08:00:00"),
				),
			},
			// Step 4: the same configuration plans no changes.
			{
				Config:   testAccBusinessHoursConfig(renamed, []string{"Saturday", "Sunday", "Wednesday"}, "8:0:0", "1.00:00:00", ""),
				PlanOnly: true,
			},
			// Step 5: every day ("Everyday" in the API), an overnight range and
			// a Windows time zone ID (stored as written).
			{
				Config: testAccBusinessHoursConfig(renamed, everyday, "22:00:00", "06:00:00", "W. Europe Standard Time"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "days_of_week.#", "7"),
					resource.TestCheckResourceAttr(addr, "start_time", "22:00:00"),
					resource.TestCheckResourceAttr(addr, "end_time", "06:00:00"),
					resource.TestCheckResourceAttr(addr, "time_zone_id", "W. Europe Standard Time"),
				),
			},
			// Step 6: plan-only again after the composite-day round trip.
			{
				Config:   testAccBusinessHoursConfig(renamed, everyday, "22:00:00", "06:00:00", "W. Europe Standard Time"),
				PlanOnly: true,
			},
		},
	})
}

// testAccBusinessHoursConfig returns a configuration with one business-hours
// resource and a data source reading it. An empty timeZoneID omits the
// attribute.
func testAccBusinessHoursConfig(name string, days []string, start, end, timeZoneID string) string {
	quoted := make([]string, len(days))
	for i, d := range days {
		quoted[i] = fmt.Sprintf("%q", d)
	}
	tz := ""
	if timeZoneID != "" {
		tz = fmt.Sprintf("\n  time_zone_id = %q", timeZoneID)
	}
	return fmt.Sprintf(`
resource "monotaur_business_hours" "test" {
  name         = %q
  days_of_week = [%s]
  start_time   = %q
  end_time     = %q%s
}

data "monotaur_business_hours" "test" {
  id = monotaur_business_hours.test.id
}
`, name, strings.Join(quoted, ", "), start, end, tz)
}

// testAccCheckAPIResourcesGone returns a CheckDestroy function asserting that
// every resource of tfType left in the final state answers GET
// /api/v1/<collection>/<id> with 404.
func testAccCheckAPIResourcesGone(tfType, collection string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
		apiKey := os.Getenv("MONOTAUR_API_KEY")
		httpClient := &http.Client{Timeout: 30 * time.Second}
		for _, rs := range s.RootModule().Resources {
			if rs.Type != tfType || rs.Primary == nil || rs.Primary.ID == "" {
				continue
			}
			url := fmt.Sprintf("%s/api/v1/%s/%s", endpoint, collection, rs.Primary.ID)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Accept", "application/vnd.api+json; ext=openapi")
			req.Header.Set("Authorization", "Bearer "+apiKey)
			resp, err := httpClient.Do(req)
			if err != nil {
				return fmt.Errorf("GET %s: %w", url, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				return fmt.Errorf("%s %s still exists after destroy: GET returned HTTP %d", tfType, rs.Primary.ID, resp.StatusCode)
			}
		}
		return nil
	}
}

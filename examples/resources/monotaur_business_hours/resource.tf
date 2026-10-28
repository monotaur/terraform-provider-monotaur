# Office hours on weekdays, on the Berlin wall clock (daylight-saving aware).
resource "monotaur_business_hours" "office" {
  name         = "Office hours"
  days_of_week = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
  start_time   = "08:00:00"
  end_time     = "18:00:00"
  time_zone_id = "Europe/Berlin"
}

# An overnight batch window: an end_time earlier than start_time ends on the
# following day. Without time_zone_id the times are UTC.
resource "monotaur_business_hours" "batch" {
  name         = "Nightly batch"
  days_of_week = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]
  start_time   = "22:00:00"
  end_time     = "06:00:00"
}

# Business hours are attached from the component side.
resource "monotaur_component" "payments" {
  name              = "Payments"
  business_hours_id = monotaur_business_hours.office.id
}

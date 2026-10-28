resource "monotaur_label" "release" {
  text = "change::release"
}

# A deployment recorded alongside status history. Labels are linked from the
# event; do not also list the event in monotaur_label.calendar_event_ids.
resource "monotaur_calendar_event" "release_4_2" {
  title              = "Release 4.2"
  description        = "Rolled out to all regions"
  external_reference = "CHG-1234"
  start_date_time    = "2030-03-14T22:00:00+01:00"
  end_date_time      = "2030-03-14T22:45:00+01:00"
  label_ids          = [monotaur_label.release.id]
}

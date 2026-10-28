resource "monotaur_component" "database" {
  name = "Database"
}

# Patch the database every Sunday from 02:00 to 04:00 Berlin time. Monotaur
# creates each occurrence's maintenance window itself, one occurrence ahead.
resource "monotaur_maintenance_schedule" "weekly_patching" {
  description               = "Weekly database patching"
  schedule                  = "0 2 * * 0"
  time_span                 = "02:00:00"
  time_zone_id              = "Europe/Berlin"
  exclude_from_downtime     = true
  apply_to_component_status = true
  squelch                   = true
  component_ids             = [monotaur_component.database.id]
}

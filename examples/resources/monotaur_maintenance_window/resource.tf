resource "monotaur_maintenance_window" "db_migration" {
  description               = "Database migration to the new cluster"
  start_date_time           = "2030-03-14T22:00:00+01:00"
  end_date_time             = "2030-03-15T02:00:00+01:00"
  reason                    = "ScheduledMaintenance"
  exclude_from_downtime     = true
  apply_to_component_status = true
  squelch                   = true
}

# Components own the window links.
resource "monotaur_component" "database" {
  name                   = "Database"
  maintenance_window_ids = [monotaur_maintenance_window.db_migration.id]
}

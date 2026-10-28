resource "monotaur_component" "api_server" {
  name = "API Server"
}

resource "monotaur_label" "team" {
  text = "team::platform"
}

# A heartbeat monitor: the service reports in every 5 minutes, with 30 seconds
# of grace before a missed heartbeat faults the monitor.
resource "monotaur_monitor" "api_health" {
  name               = "API Health Check"
  type               = "Heartbeat"
  heartbeat_interval = "00:05:00"
  heartbeat_grace    = "00:00:30"
  component_ids      = [monotaur_component.api_server.id]
  label_ids          = [monotaur_label.team.id]
}

# A probed monitor with flap suppression: three faulted samples spanning at
# least two minutes open an alarm, and a probe that misses five runs faults it.
resource "monotaur_monitor" "website" {
  name                     = "Website"
  type                     = "Alert"
  fault_threshold          = 3
  recovery_threshold       = 2
  confirmation_delay       = "00:02:00"
  flapping_threshold       = 6
  no_data_status           = "Fault"
  no_data_missed_intervals = 5
  enabled                  = true
}

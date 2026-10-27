resource "monotaur_monitor" "api_health" {
  name = "API Health Check"
  type = "Alert"
}

resource "monotaur_probe" "http" {
  monitor_id   = monotaur_monitor.api_health.id
  active       = true
  schedule     = "*/5 8-18 * * 1-5"
  time_zone_id = "Europe/Berlin"
}

# Sensors attach to a probe through their own probe_id; the probe's
# sensor_ids attribute is read-only.

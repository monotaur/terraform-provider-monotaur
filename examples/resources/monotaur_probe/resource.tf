resource "monotaur_monitor" "api_health" {
  name = "API Health Check"
  type = "Heartbeat"
}

resource "monotaur_probe" "http" {
  monitor_id = monotaur_monitor.api_health.id
  active     = true
  schedule   = "*/5 * * * *"
}

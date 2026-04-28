resource "monotaur_monitor" "api_health" {
  name = "API Health Check"
  type = "Heartbeat"
}

resource "monotaur_monitor_status_rule" "degraded" {
  monitor_id     = monotaur_monitor.api_health.id
  predicate      = "latency > 500"
  status         = "Degraded"
  status_message = "Response time exceeds 500 ms"
}

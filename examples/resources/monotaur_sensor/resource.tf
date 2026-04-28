resource "monotaur_monitor" "api_health" {
  name = "API Health Check"
  type = "Heartbeat"
}

resource "monotaur_probe" "http" {
  monitor_id = monotaur_monitor.api_health.id
  active     = true
  schedule   = "*/5 * * * *"
}

resource "monotaur_sensor" "http_check" {
  probe_id    = monotaur_probe.http.id
  name        = "HTTP Check"
  plugin_name = "http"
  type        = "HttpCheck"
  parameters  = jsonencode({ url = "https://api.example.com/health" })
}

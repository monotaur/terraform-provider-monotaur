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
  plugin_name = "NetSensors"
  type        = "HttpSensor"
  parameters  = jsonencode({ Url = "https://api.example.com/health" })
}

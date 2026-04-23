resource "monotaur_monitor" "api_health" {
  name = "API Health Check"
  type = "Heartbeat"
}

# Record a known-outage window as an alarm so it is excluded from downtime
# calculations. Alarm start/end are optional; omitting end_date_time leaves
# the alarm open until the API closes it.
resource "monotaur_alarm" "scheduled_maintenance" {
  monitor_id           = monotaur_monitor.api_health.id
  start_date_time      = "2025-01-15T02:00:00Z"
  end_date_time        = "2025-01-15T04:00:00Z"
  exclude_from_downtime = true
  squelch              = true
}

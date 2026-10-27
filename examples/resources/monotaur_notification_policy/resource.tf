resource "monotaur_label" "production" {
  text = "env_production"
}

resource "monotaur_notification_channel" "prod_oncall" {
  name        = "#prod-oncall"
  type        = "Webhook"
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
  credential  = "https://hooks.example.com/prod-oncall"
}

# Send production alarms of severity Error or above to #prod-oncall, and
# remind the channel every 15 minutes until the alarm is acknowledged.
resource "monotaur_notification_policy" "production" {
  name                    = "Production on-call"
  minimum_severity        = "Error"
  repeat_interval_minutes = 15
  channel_ids             = [monotaur_notification_channel.prod_oncall.id]
  label_ids               = [monotaur_label.production.id]
}

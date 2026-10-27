# Slack: the incoming webhook URL is the credential.
resource "monotaur_notification_channel" "oncall_slack" {
  name        = "On-call Slack"
  type        = "Slack"
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
  credential  = var.slack_webhook_url
}

# Email: no credential; 1 to 20 recipients; optional custom subject.
resource "monotaur_notification_channel" "ops_email" {
  name             = "Ops email"
  type             = "Email"
  event_types      = ["com.monotaur.component.status-changed"]
  recipients       = ["ops@example.com", "On-call <oncall@example.com>"]
  subject_template = "[Monotaur] {{ summary }}"
}

# Generic webhook with a custom JSON body (must render valid JSON).
resource "monotaur_notification_channel" "webhook" {
  name          = "Incident bridge"
  type          = "Webhook"
  event_types   = ["com.monotaur.alarm.triggered"]
  credential    = "https://example.com/hooks/monotaur"
  body_template = "{\"summary\": {{ summary | object.to_json }}}"
  enabled       = true
}

variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

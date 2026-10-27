resource "monotaur_notification_channel" "team_lead" {
  name        = "Team lead"
  type        = "Webhook"
  event_types = ["com.monotaur.alarm.triggered", "com.monotaur.alarm.cleared"]
  credential  = "https://hooks.example.com/team-lead"
}

resource "monotaur_notification_policy" "production" {
  name             = "Production on-call"
  minimum_severity = "Error"
}

# Notify the team lead if an alarm the policy matches is still
# unacknowledged 30 minutes after it was reported.
resource "monotaur_notification_escalation_step" "team_lead" {
  notification_policy_id = monotaur_notification_policy.production.id
  delay_minutes          = 30
  channel_ids            = [monotaur_notification_channel.team_lead.id]
}

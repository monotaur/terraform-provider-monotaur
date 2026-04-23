resource "monotaur_service_account" "ci_bot" {
  name = "ci-bot"
}

resource "monotaur_role" "monitor_editor" {
  name = "monitor-editor"
  permissions = [
    "monitors:read",
    "monitors:create",
    "monitors:update",
  ]
}

resource "monotaur_role_assignment" "ci_bot_editor" {
  service_account_id = monotaur_service_account.ci_bot.id
  role_id            = monotaur_role.monitor_editor.id
}

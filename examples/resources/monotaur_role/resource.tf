resource "monotaur_role" "monitor_editor" {
  name        = "monitor-editor"
  description = "Can create and update monitors but cannot delete them"
  permissions = [
    "monitors:read",
    "monitors:create",
    "monitors:update",
  ]
}

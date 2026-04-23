resource "monotaur_secret" "webhook_token" {
  name        = "webhook_signing_token"
  value       = var.webhook_token
  description = "HMAC token used to verify incoming webhook payloads"
}

variable "webhook_token" {
  type      = string
  sensitive = true
}

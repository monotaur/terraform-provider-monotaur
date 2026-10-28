# Supply your own signing key (at least 32 bytes of UTF-8) and share it with the receiver.
resource "monotaur_webhook_subscription" "incidents" {
  url = "https://hooks.example.com/monotaur"
  event_types = [
    "com.monotaur.alarm.triggered",
    "com.monotaur.alarm.cleared",
    "com.monotaur.alarm.acknowledged",
  ]
  payload_mode = "ChangesOnly"
  signing_key  = var.webhook_signing_key
}

# Or let Monotaur generate the key: it is exposed once, at creation, as
# generated_signing_key (null after `terraform import`).
resource "monotaur_webhook_subscription" "audit" {
  url         = "https://audit.example.com/monotaur"
  event_types = ["com.monotaur.monitor.created", "com.monotaur.monitor.deleted"]
}

output "audit_signing_key" {
  value     = monotaur_webhook_subscription.audit.generated_signing_key
  sensitive = true
}

variable "webhook_signing_key" {
  type      = string
  sensitive = true
}

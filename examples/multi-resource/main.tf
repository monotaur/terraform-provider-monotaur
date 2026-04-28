###############################################################################
# Multi-resource example
#
# This example provisions a complete monitoring setup for a web service:
#
#   label → component → monitor → probe → sensor
#                                └→ monitor_status_rule
#
# It also creates a service account with an API key and the least-privilege
# role needed for a CI/CD bot to report heartbeats.
###############################################################################

terraform {
  required_providers {
    monotaur = {
      source  = "registry.terraform.io/monotaur/monotaur"
      version = "~> 0.1"
    }
  }
}

provider "monotaur" {
  # endpoint and api_key are read from MONOTAUR_ENDPOINT and
  # MONOTAUR_API_KEY environment variables when not specified here.
}

# ---------------------------------------------------------------------------
# Labels
# ---------------------------------------------------------------------------

resource "monotaur_label" "env_production" {
  text  = "production"
  color = "#E53935"
  icon  = "🚩"
}

resource "monotaur_label" "team_platform" {
  text  = "platform"
  color = "#1E88E5"
}

# ---------------------------------------------------------------------------
# Component
# ---------------------------------------------------------------------------

resource "monotaur_component" "api_server" {
  name      = "API Server"
  label_ids = [monotaur_label.env_production.id, monotaur_label.team_platform.id]
}

# ---------------------------------------------------------------------------
# Monitor + status rule
# ---------------------------------------------------------------------------

resource "monotaur_monitor" "api_health" {
  name          = "API Server Health"
  type          = "Heartbeat"
  component_ids = [monotaur_component.api_server.id]
}

resource "monotaur_monitor_status_rule" "degraded" {
  monitor_id     = monotaur_monitor.api_health.id
  predicate      = "latency > 500"
  status         = "Degraded"
  status_message = "Response time exceeds 500 ms"
}

resource "monotaur_monitor_status_rule" "down" {
  monitor_id     = monotaur_monitor.api_health.id
  predicate      = "no_heartbeat_for > 120"
  status         = "Down"
  status_message = "No heartbeat received in the last 2 minutes"
}

# ---------------------------------------------------------------------------
# Probe + sensor
# ---------------------------------------------------------------------------

resource "monotaur_probe" "http" {
  monitor_id = monotaur_monitor.api_health.id
  active     = true
  schedule   = "*/5 * * * *"
}

resource "monotaur_sensor" "http_check" {
  probe_id    = monotaur_probe.http.id
  name        = "HTTP GET /health"
  plugin_name = "http"
  type        = "HttpCheck"
  parameters  = jsonencode({ url = "https://api.example.com/health" })
}

# ---------------------------------------------------------------------------
# Variables and secrets
# ---------------------------------------------------------------------------

resource "monotaur_variable" "latency_threshold" {
  name        = "latency_threshold_ms"
  value       = "500"
  description = "Maximum acceptable API response time in milliseconds"
  monitor_id  = monotaur_monitor.api_health.id
}

resource "monotaur_secret" "webhook_token" {
  name        = "webhook_signing_token"
  value       = var.webhook_token
  description = "HMAC token for webhook payload verification"
}

# ---------------------------------------------------------------------------
# Service account, role, and API key for a CI/CD bot
# ---------------------------------------------------------------------------

resource "monotaur_service_account" "ci_bot" {
  name        = "ci-bot"
  description = "Service account used by the CI/CD pipeline to report heartbeats"
}

resource "monotaur_role" "heartbeat_reporter" {
  name        = "heartbeat-reporter"
  description = "Minimal permissions required to send heartbeats"
  permissions = [
    "monitors:read",
    "probes:read",
    "sensors:write",
  ]
}

resource "monotaur_role_assignment" "ci_bot_reporter" {
  service_account_id = monotaur_service_account.ci_bot.id
  role_id            = monotaur_role.heartbeat_reporter.id
}

resource "monotaur_api_key" "ci_bot_key" {
  name               = "ci-bot-production-key"
  environment        = "production"
  service_account_id = monotaur_service_account.ci_bot.id

  # api_key depends on the role assignment so the key is created after
  # the service account already has the required permissions.
  depends_on = [monotaur_role_assignment.ci_bot_reporter]
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "monitor_id" {
  value       = monotaur_monitor.api_health.id
  description = "ID of the provisioned monitor."
}

output "ci_bot_key_prefix" {
  value       = monotaur_api_key.ci_bot_key.prefix
  description = "Non-sensitive prefix of the CI bot API key (for identification)."
}

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

variable "webhook_token" {
  type      = string
  sensitive = true
  description = "HMAC token for webhook payload verification. Prefer passing via TF_VAR_webhook_token."
}

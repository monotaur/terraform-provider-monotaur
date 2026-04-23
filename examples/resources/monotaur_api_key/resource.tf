resource "monotaur_service_account" "ci_bot" {
  name = "ci-bot"
}

resource "monotaur_api_key" "ci_bot_key" {
  name               = "ci-bot-key"
  environment        = "production"
  service_account_id = monotaur_service_account.ci_bot.id
}

# The key value is only available immediately after creation.
# Store it in a secret store rather than an output to avoid exposure.
output "ci_bot_key_prefix" {
  value       = monotaur_api_key.ci_bot_key.prefix
  description = "Non-sensitive prefix of the created API key (for identification)."
}

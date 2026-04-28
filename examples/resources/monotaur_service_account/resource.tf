resource "monotaur_service_account" "ci_bot" {
  name        = "ci-bot"
  description = "Service account used by the CI/CD pipeline"
  disabled    = false
}

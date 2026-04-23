resource "monotaur_variable" "threshold" {
  name        = "response_time_threshold"
  value       = "500"
  description = "Maximum acceptable response time in milliseconds"
}

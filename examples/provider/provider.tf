terraform {
  required_providers {
    monotaur = {
      source  = "registry.terraform.io/monotaur/monotaur"
      version = "~> 0.1"
    }
  }
}

# Configure the provider using explicit values.
# Alternatively, set MONOTAUR_ENDPOINT and MONOTAUR_API_KEY environment variables
# and omit these attributes entirely.
provider "monotaur" {
  endpoint = "https://api.monotaur.io"
  api_key  = var.monotaur_api_key
}

variable "monotaur_api_key" {
  type      = string
  sensitive = true
  description = "Monotaur API key. Prefer the MONOTAUR_API_KEY environment variable."
}

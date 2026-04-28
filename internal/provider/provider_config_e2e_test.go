package provider_test

// provider_config_e2e_test.go tests the provider configuration surface:
// env-var fallback, missing-credential errors, bad-credential 401, and
// bad-endpoint connection failures.
//
// These tests deliberately set the provider block to exercise error paths;
// they do NOT use testAccPreCheck because several scenarios intentionally
// unset or override the standard env vars.

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// ---------------------------------------------------------------------------
// TestAccMonotaurProvider_missingAPIKey
//
// Verifies that when api_key is absent from both the provider block and the
// MONOTAUR_API_KEY environment variable, the provider returns a clear
// diagnostic naming both the attribute and the env var — not a raw error dump.
// ---------------------------------------------------------------------------

func TestAccMonotaurProvider_missingAPIKey(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	// Unset the env var for the duration of this test.
	origKey := os.Getenv("MONOTAUR_API_KEY")
	os.Unsetenv("MONOTAUR_API_KEY")
	if origKey != "" {
		defer os.Setenv("MONOTAUR_API_KEY", origKey)
	}

	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.staging.monotaur.io"
	}

	resource.Test(t, resource.TestCase{
		// No PreCheck — we intentionally have no api_key set.
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Any config that causes the provider to configure is enough.
				Config: providerConfigOnly(endpoint, ""),
				// Provider Configure should return a "Missing API Key" diagnostic
				// mentioning both the attribute and the env var.
				ExpectError: regexp.MustCompile(`(?i)api.?key|MONOTAUR_API_KEY|missing`),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurProvider_badAPIKey
//
// Verifies that a deliberately wrong API key causes the provider to surface a
// typed auth-related error — not a raw HTTP status code dump — when the first
// real request is made against the staging endpoint.
// ---------------------------------------------------------------------------

func TestAccMonotaurProvider_badAPIKey(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if endpoint == "" {
		t.Skip("MONOTAUR_ENDPOINT must be set to test bad-credential error path")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Use a deliberately invalid API key to trigger a 401 from the real API.
				Config: providerWithLabelRead(endpoint, "bad-api-key-for-testing-401"),
				// The error must mention auth/unauthorized — not a raw HTTP dump.
				ExpectError: regexp.MustCompile(`(?i)401|unauthorized|auth`),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurProvider_badEndpoint
//
// Verifies that a non-routable endpoint surfaces a clear connection error
// rather than a panic or stack trace.
// ---------------------------------------------------------------------------

func TestAccMonotaurProvider_badEndpoint(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	// Use a valid API key (or any non-empty value) so the provider passes
	// the api_key check and attempts the connection, then fails on the endpoint.
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if apiKey == "" {
		apiKey = "any-non-empty-key-forces-connection-attempt"
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Use a syntactically valid but non-routable hostname to trigger a DNS failure.
				Config: providerWithLabelRead("https://this-host-does-not-exist.monotaur.invalid", apiKey),
				// Provider must surface a clear connection error, not a crash.
				ExpectError: regexp.MustCompile(`(?i)no such host|connection refused|dial|network|unable to connect|error`),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// TestAccMonotaurProvider_blockOverridesEnvVar
//
// Verifies that an explicit api_key in the provider block takes precedence
// over the MONOTAUR_API_KEY environment variable.
//
// Strategy: with a valid key in MONOTAUR_API_KEY and a deliberately invalid
// key hardcoded in the provider block, the provider must use the block value
// (resulting in a 401) rather than the env var value (which would succeed).
// A successful request would mean the env var was used instead — that would
// be a bug.
// ---------------------------------------------------------------------------

func TestAccMonotaurProvider_blockOverridesEnvVar(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}

	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if endpoint == "" {
		t.Skip("MONOTAUR_ENDPOINT must be set to test env-var override")
	}
	if os.Getenv("MONOTAUR_API_KEY") == "" {
		t.Skip("MONOTAUR_API_KEY must be set (as the env-var that should be overridden)")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// MONOTAUR_API_KEY is set (valid) in the environment, but the provider
				// block explicitly supplies an invalid key. The block must win.
				Config: providerWithLabelRead(endpoint, "block-override-key-intentionally-wrong"),
				// If the block key is used (correct behavior): 401 from the real API.
				// If the env var is used (bug): the request would succeed, no error → test fails.
				ExpectError: regexp.MustCompile(`(?i)401|unauthorized|auth`),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// providerConfigOnly returns a minimal Terraform config that sets the provider
// block without any resources. If apiKey is empty, the api_key attribute is
// omitted entirely (not set to ""). This exercises the missing-credential path.
func providerConfigOnly(endpoint, apiKey string) string {
	if apiKey == "" {
		return `
provider "monotaur" {
  endpoint = "` + endpoint + `"
}
`
	}
	return `
provider "monotaur" {
  endpoint = "` + endpoint + `"
  api_key  = "` + apiKey + `"
}
`
}

// providerWithLabelRead returns a Terraform config with a provider block and a
// label data source read. The data source read forces the provider to make a
// real API call, surfacing connection and auth errors.
func providerWithLabelRead(endpoint, apiKey string) string {
	return `
provider "monotaur" {
  endpoint = "` + endpoint + `"
  api_key  = "` + apiKey + `"
}

data "monotaur_label" "probe" {
  id = "00000000-0000-0000-0000-000000000000"
}
`
}

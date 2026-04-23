package provider_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// testAccProtoV6ProviderFactories is used by acceptance tests to create a
// protocol v6 provider factory wired to the local provider implementation.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"monotaur": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// testAccPreCheck validates that required environment variables are set before
// running an acceptance test. Call t.Helper() so failures point to the caller.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if v := os.Getenv("MONOTAUR_ENDPOINT"); v == "" {
		t.Fatal("MONOTAUR_ENDPOINT must be set for acceptance tests")
	}
	if v := os.Getenv("MONOTAUR_API_KEY"); v == "" {
		t.Fatal("MONOTAUR_API_KEY must be set for acceptance tests")
	}
}

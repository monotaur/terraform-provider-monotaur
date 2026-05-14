package provider_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenApiKey unit tests
// ---------------------------------------------------------------------------

// apiKeyFlattenFixture builds a DataInAdminApiKeyResponse with all fields populated.
// Note: the plaintext (key_value) field is intentionally omitted — it mirrors
// what the API returns on GET (the key value is only in the create response).
func apiKeyFlattenFixture() api.DataInAdminApiKeyResponse {
	now := time.Now()
	name := "my-api-key"
	environment := "production"
	prefix := "mtak_"
	createdBy := "admin@example.com"
	saType := api.AdminServiceAccountResourceType("admin.serviceAccounts")

	return api.DataInAdminApiKeyResponse{
		Id: "key-1",
		Attributes: &api.AttributesInAdminApiKeyResponse{
			Name:        &name,
			Environment: &environment,
			Prefix:      &prefix,
			CreatedAt:   &now,
			CreatedBy:   &createdBy,
			// Plaintext is intentionally omitted (only returned on create)
		},
		Relationships: &api.RelationshipsInAdminApiKeyResponse{
			ServiceAccount: &api.ToOneAdminServiceAccountInResponse{
				Data: &api.AdminServiceAccountIdentifierInResponse{
					Id:   "sa-1",
					Type: saType,
				},
			},
		},
	}
}

func TestFlattenApiKey_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Id = "key-99"

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "key-99" {
		t.Errorf("ID: want %q, got %q", "key-99", got)
	}
}

func TestFlattenApiKey_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "my-api-key" {
		t.Errorf("Name: want %q, got %q", "my-api-key", got)
	}
}

func TestFlattenApiKey_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenApiKey_setsEnvironmentFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.Environment.ValueString(); got != "production" {
		t.Errorf("Environment: want %q, got %q", "production", got)
	}
}

func TestFlattenApiKey_setsPrefixFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.Prefix.ValueString(); got != "mtak_" {
		t.Errorf("Prefix: want %q, got %q", "mtak_", got)
	}
}

func TestFlattenApiKey_setsCreatedByFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.CreatedBy.ValueString(); got != "admin@example.com" {
		t.Errorf("CreatedBy: want %q, got %q", "admin@example.com", got)
	}
}

func TestFlattenApiKey_setsCreatedAtTimestamp(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if model.CreatedAt.IsNull() || model.CreatedAt.ValueString() == "" {
		t.Error("CreatedAt: expected non-empty value")
	}
}

func TestFlattenApiKey_createdAtIsRFC3339Formatted(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if _, err := time.Parse(time.RFC3339, model.CreatedAt.ValueString()); err != nil {
		t.Errorf("CreatedAt: value %q is not valid RFC3339: %v", model.CreatedAt.ValueString(), err)
	}
}

func TestFlattenApiKey_nullsCreatedAtWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Attributes.CreatedAt = nil

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if !model.CreatedAt.IsNull() {
		t.Errorf("CreatedAt: expected null, got %q", model.CreatedAt.ValueString())
	}
}

func TestFlattenApiKey_setsExpiresAtWhenPresent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	future := time.Now().Add(24 * time.Hour)
	fixture.Attributes.ExpiresAt = &future

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if model.ExpiresAt.IsNull() || model.ExpiresAt.ValueString() == "" {
		t.Error("ExpiresAt: expected non-empty value when set")
	}
	if _, err := time.Parse(time.RFC3339, model.ExpiresAt.ValueString()); err != nil {
		t.Errorf("ExpiresAt: value %q is not valid RFC3339: %v", model.ExpiresAt.ValueString(), err)
	}
}

func TestFlattenApiKey_nullsExpiresAtWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	// ExpiresAt is nil in the fixture by default

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if !model.ExpiresAt.IsNull() {
		t.Errorf("ExpiresAt: expected null, got %q", model.ExpiresAt.ValueString())
	}
}

func TestFlattenApiKey_setsLastUsedAtWhenPresent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	lastUsed := time.Now().Add(-1 * time.Hour)
	fixture.Attributes.LastUsedAt = &lastUsed

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if model.LastUsedAt.IsNull() || model.LastUsedAt.ValueString() == "" {
		t.Error("LastUsedAt: expected non-empty value when set")
	}
	if _, err := time.Parse(time.RFC3339, model.LastUsedAt.ValueString()); err != nil {
		t.Errorf("LastUsedAt: value %q is not valid RFC3339: %v", model.LastUsedAt.ValueString(), err)
	}
}

func TestFlattenApiKey_nullsLastUsedAtWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if !model.LastUsedAt.IsNull() {
		t.Errorf("LastUsedAt: expected null, got %q", model.LastUsedAt.ValueString())
	}
}

func TestFlattenApiKey_setsRevokedFieldsWhenPresent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	revokedAt := time.Now()
	revokedBy := "admin@example.com"
	fixture.Attributes.RevokedAt = &revokedAt
	fixture.Attributes.RevokedBy = &revokedBy

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if model.RevokedAt.IsNull() || model.RevokedAt.ValueString() == "" {
		t.Error("RevokedAt: expected non-empty value when set")
	}
	if model.RevokedBy.ValueString() != "admin@example.com" {
		t.Errorf("RevokedBy: want %q, got %q", "admin@example.com", model.RevokedBy.ValueString())
	}
}

func TestFlattenApiKey_nullsRevokedFieldsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if !model.RevokedAt.IsNull() {
		t.Errorf("RevokedAt: expected null, got %q", model.RevokedAt.ValueString())
	}
	if !model.RevokedBy.IsNull() {
		t.Errorf("RevokedBy: expected null, got %q", model.RevokedBy.ValueString())
	}
}

func TestFlattenApiKey_setsServiceAccountIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ServiceAccountID.ValueString(); got != "sa-1" {
		t.Errorf("ServiceAccountID: want %q, got %q", "sa-1", got)
	}
}

// JSON:API responses omit relationship `data` unless explicitly included, so
// flattenApiKey preserves the caller-supplied model value when the response
// has no relationship data. The following three tests verify that preservation
// across the three "data absent" shapes.
func TestFlattenApiKey_preservesServiceAccountIDWhenRelationshipsAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Relationships = nil

	model := provider.ApiKeyResourceModelForTest{ServiceAccountID: types.StringValue("sa-preserved")}
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ServiceAccountID.ValueString(); got != "sa-preserved" {
		t.Errorf("ServiceAccountID: want %q (preserved), got %q", "sa-preserved", got)
	}
}

func TestFlattenApiKey_preservesServiceAccountIDWhenServiceAccountAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Relationships.ServiceAccount = nil

	model := provider.ApiKeyResourceModelForTest{ServiceAccountID: types.StringValue("sa-preserved")}
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ServiceAccountID.ValueString(); got != "sa-preserved" {
		t.Errorf("ServiceAccountID: want %q (preserved), got %q", "sa-preserved", got)
	}
}

func TestFlattenApiKey_preservesServiceAccountIDWhenServiceAccountDataAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	fixture.Relationships.ServiceAccount = &api.ToOneAdminServiceAccountInResponse{
		Data: nil,
	}

	model := provider.ApiKeyResourceModelForTest{ServiceAccountID: types.StringValue("sa-preserved")}
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ServiceAccountID.ValueString(); got != "sa-preserved" {
		t.Errorf("ServiceAccountID: want %q (preserved), got %q", "sa-preserved", got)
	}
}

func TestFlattenApiKey_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInAdminApiKeyResponse{
		Id:         "key-1",
		Attributes: nil,
	}

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "key-1" {
		t.Errorf("ID: want %q, got %q", "key-1", got)
	}
}

// TestFlattenApiKey_preservesKeyValueWhenAPIOmitsPlaintext verifies the critical
// write-once behavior: flattenApiKey must NOT overwrite model.KeyValue.
// This ensures the key value captured at create time is preserved in state
// across subsequent Read calls (where the API returns no plaintext).
func TestFlattenApiKey_preservesKeyValueWhenAPIOmitsPlaintext(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()
	// Plaintext is nil in the fixture, simulating a GET response.

	model := provider.ApiKeyResourceModelForTest{
		KeyValue: types.StringValue("mtak_super-secret-key-value"),
	}
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	if got := model.KeyValue.ValueString(); got != "mtak_super-secret-key-value" {
		t.Errorf("KeyValue: expected prior state value %q to be preserved, got %q",
			"mtak_super-secret-key-value", got)
	}
}

// TestFlattenApiKey_leavesKeyValueUnchangedWhenModelStartsEmpty verifies that
// flattenApiKey does not set key_value to a non-null value when the model starts
// empty (e.g. after import). The API does not return the key value on read;
// the field should remain at its zero value.
func TestFlattenApiKey_leavesKeyValueUnchangedWhenModelStartsEmpty(t *testing.T) {
	ctx := context.Background()
	fixture := apiKeyFlattenFixture()

	var model provider.ApiKeyResourceModelForTest
	diags := provider.FlattenApiKeyForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenApiKeyForTest returned errors: %v", diags)
	}
	// KeyValue should still be null (zero value of types.String).
	if model.KeyValue.ValueString() != "" {
		t.Errorf("KeyValue: expected empty/null when no prior state, got %q", model.KeyValue.ValueString())
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestApiKeyResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewApiKeyResource()
	if res == nil {
		t.Fatal("NewApiKeyResource returned nil")
	}
}

func TestApiKeyDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewApiKeyDataSource()
	if ds == nil {
		t.Fatal("NewApiKeyDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Acceptance tests (env-gated via TF_ACC=1)
//
// These tests require a live Monotaur API endpoint. They are compiled here so
// `go build ./...` succeeds; they will only execute when TF_ACC=1 is set.
// ---------------------------------------------------------------------------
//
// To run acceptance tests:
//
//	TF_ACC=1 MONOTAUR_API_KEY=<key> MONOTAUR_ENDPOINT=<url> go test ./internal/provider/ -run TestAcc -v

func TestAccApiKeyResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → import → destroy).
	// 1. Create a service account.
	// 2. Create an API key for that service account.
	// 3. Verify key_value is non-empty immediately after create.
	// 4. Run terraform plan — verify no diff (UseStateForUnknown preserves key_value).
	// 5. Verify import by ID: key_value is empty after import (not recoverable).
	// 6. Destroy: verify the key is removed from the service account's key list.
}

// TestAccApiKeyDataSource_basic verifies the data source lookup by ID.
func TestAccApiKeyDataSource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test.
	// 1. Create a service account and API key via the resource.
	// 2. Look up the key via the data source using the key ID.
	// 3. Verify name, environment, service_account_id, prefix round-trip correctly.
	// 4. Verify key_value is empty in the data source (API does not return it on read).
}

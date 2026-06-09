package provider_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenServiceAccount unit tests
// ---------------------------------------------------------------------------

// serviceAccountFlattenFixture builds a DataInAdminServiceAccountResponse with all fields populated.
func serviceAccountFlattenFixture() api.DataInAdminServiceAccountResponse {
	name := "ci-bot"
	description := "Continuous-integration bot service account"
	disabled := false
	createdAt := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	return api.DataInAdminServiceAccountResponse{
		Id: "sa-1",
		Attributes: &api.AttributesInAdminServiceAccountResponse{
			Name:        &name,
			Description: &description,
			Disabled:    &disabled,
			CreatedAt:   &createdAt,
		},
	}
}

func TestFlattenServiceAccount_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	fixture.Id = "sa-99"

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "sa-99" {
		t.Errorf("ID: want %q, got %q", "sa-99", got)
	}
}

func TestFlattenServiceAccount_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "ci-bot" {
		t.Errorf("Name: want %q, got %q", "ci-bot", got)
	}
}

func TestFlattenServiceAccount_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenServiceAccount_setsDescriptionFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if got := model.Description.ValueString(); got != "Continuous-integration bot service account" {
		t.Errorf("Description: want %q, got %q", "Continuous-integration bot service account", got)
	}
}

func TestFlattenServiceAccount_nullsDescriptionWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	fixture.Attributes.Description = nil

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if !model.Description.IsNull() {
		t.Errorf("Description: expected null, got %q", model.Description.ValueString())
	}
}

func TestFlattenServiceAccount_setsDisabledFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	disabled := true
	fixture.Attributes.Disabled = &disabled

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if !model.Disabled.ValueBool() {
		t.Errorf("Disabled: want true, got false")
	}
}

func TestFlattenServiceAccount_nullsDisabledWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	fixture.Attributes.Disabled = nil

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if !model.Disabled.IsNull() {
		t.Errorf("Disabled: expected null, got %v", model.Disabled.ValueBool())
	}
}

func TestFlattenServiceAccount_setsCreatedAtRFC3339Formatted(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	want := "2026-06-04T12:00:00Z"
	if got := model.CreatedAt.ValueString(); got != want {
		t.Errorf("CreatedAt: want %q, got %q", want, got)
	}
}

func TestFlattenServiceAccount_nullsCreatedAtWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := serviceAccountFlattenFixture()
	fixture.Attributes.CreatedAt = nil

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if !model.CreatedAt.IsNull() {
		t.Errorf("CreatedAt: expected null, got %q", model.CreatedAt.ValueString())
	}
}

func TestFlattenServiceAccount_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInAdminServiceAccountResponse{
		Id:         "sa-1",
		Attributes: nil,
	}

	var model provider.ServiceAccountResourceModelForTest
	diags := provider.FlattenServiceAccountForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenServiceAccountForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "sa-1" {
		t.Errorf("ID: want %q, got %q", "sa-1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestServiceAccountResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewServiceAccountResource()
	if res == nil {
		t.Fatal("NewServiceAccountResource returned nil")
	}
}

func TestServiceAccountDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewServiceAccountDataSource()
	if ds == nil {
		t.Fatal("NewServiceAccountDataSource returned nil")
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

func TestAccServiceAccountResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify name, description, disabled round-trip and created_at is populated.
}

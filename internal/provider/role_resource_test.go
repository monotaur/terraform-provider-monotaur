package provider_test

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenRole unit tests
// ---------------------------------------------------------------------------

// roleFlattenFixture builds a DataInAdminRoleResponse with all fields populated.
func roleFlattenFixture() api.DataInAdminRoleResponse {
	name := "my-role"
	description := "A test role"
	permissions := []string{"read:monitors", "write:monitors"}

	return api.DataInAdminRoleResponse{
		Id: "role-1",
		Attributes: &api.AttributesInAdminRoleResponse{
			Name:        &name,
			Description: &description,
			Permissions: &permissions,
		},
	}
}

func TestFlattenRole_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()
	fixture.Id = "role-99"

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "role-99" {
		t.Errorf("ID: want %q, got %q", "role-99", got)
	}
}

func TestFlattenRole_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "my-role" {
		t.Errorf("Name: want %q, got %q", "my-role", got)
	}
}

func TestFlattenRole_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenRole_setsDescriptionFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if got := model.Description.ValueString(); got != "A test role" {
		t.Errorf("Description: want %q, got %q", "A test role", got)
	}
}

func TestFlattenRole_nullsDescriptionWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()
	fixture.Attributes.Description = nil

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if !model.Description.IsNull() {
		t.Errorf("Description: expected null, got %q", model.Description.ValueString())
	}
}

func TestFlattenRole_setsPermissionsFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}

	var got []string
	diags = model.Permissions.ElementsAs(ctx, &got, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}

	want := []string{"read:monitors", "write:monitors"}
	if len(got) != len(want) {
		t.Fatalf("Permissions: want %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Permissions[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

func TestFlattenRole_setsEmptyPermissionsWhenNil(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()
	fixture.Attributes.Permissions = nil

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}

	if model.Permissions.IsNull() {
		t.Error("Permissions: expected empty list, got null")
	}
	if len(model.Permissions.Elements()) != 0 {
		t.Errorf("Permissions: expected 0 elements, got %d", len(model.Permissions.Elements()))
	}
}

func TestFlattenRole_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInAdminRoleResponse{
		Id:         "role-1",
		Attributes: nil,
	}

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "role-1" {
		t.Errorf("ID: want %q, got %q", "role-1", got)
	}
}

func TestFlattenRole_setsPermissionsWithMultipleEntries(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()
	perms := []string{"read:all", "write:all", "delete:all"}
	fixture.Attributes.Permissions = &perms

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}

	var got []string
	diags = model.Permissions.ElementsAs(ctx, &got, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}

	if len(got) != 3 {
		t.Errorf("Permissions: want 3 elements, got %d: %v", len(got), got)
	}
}

func TestFlattenRole_permissionsListTypeIsString(t *testing.T) {
	ctx := context.Background()
	fixture := roleFlattenFixture()

	var model provider.RoleResourceModelForTest
	diags := provider.FlattenRoleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleForTest returned errors: %v", diags)
	}

	if model.Permissions.ElementType(ctx) != types.StringType {
		t.Errorf("Permissions element type: want types.StringType, got %T", model.Permissions.ElementType(ctx))
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestRoleResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewRoleResource()
	if res == nil {
		t.Fatal("NewRoleResource returned nil")
	}
}

func TestRoleDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewRoleDataSource()
	if ds == nil {
		t.Fatal("NewRoleDataSource returned nil")
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

func TestAccRoleResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify name, description, and permissions round-trip.
	// Verify updating permissions replaces the full list correctly.
}

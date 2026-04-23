package provider_test

import (
	"os"
	"testing"
	"time"

	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenRoleAssignment unit tests
// ---------------------------------------------------------------------------

// roleAssignmentFlattenFixture builds a DataInAdminRoleAssignmentResponse with all fields populated.
func roleAssignmentFlattenFixture() api.DataInAdminRoleAssignmentResponse {
	now := time.Now()
	roleType := api.AdminRoleResourceType("admin.roles")
	saType := api.AdminServiceAccountResourceType("admin.serviceAccounts")

	return api.DataInAdminRoleAssignmentResponse{
		Id: "ra-1",
		Attributes: &api.AttributesInAdminRoleAssignmentResponse{
			AssignedAt: &now,
		},
		Relationships: &api.RelationshipsInAdminRoleAssignmentResponse{
			Role: &api.ToOneAdminRoleInResponse{
				Data: &api.AdminRoleIdentifierInResponse{
					Id:   "role-1",
					Type: roleType,
				},
			},
			ServiceAccount: &api.ToOneAdminServiceAccountInResponse{
				Data: &api.AdminServiceAccountIdentifierInResponse{
					Id:   "sa-1",
					Type: saType,
				},
			},
		},
	}
}

func TestFlattenRoleAssignment_setsIDFromResponse(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Id = "ra-99"

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "ra-99" {
		t.Errorf("ID: want %q, got %q", "ra-99", got)
	}
}

func TestFlattenRoleAssignment_setsRoleIDFromRelationships(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if got := model.RoleID.ValueString(); got != "role-1" {
		t.Errorf("RoleID: want %q, got %q", "role-1", got)
	}
}

func TestFlattenRoleAssignment_setsServiceAccountIDFromRelationships(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if got := model.ServiceAccountID.ValueString(); got != "sa-1" {
		t.Errorf("ServiceAccountID: want %q, got %q", "sa-1", got)
	}
}

func TestFlattenRoleAssignment_setsAssignedAtTimestamp(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if model.AssignedAt.IsNull() || model.AssignedAt.ValueString() == "" {
		t.Error("AssignedAt: expected non-empty value")
	}
}

func TestFlattenRoleAssignment_assignedAtIsRFC3339Formatted(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}

	if _, err := time.Parse(time.RFC3339, model.AssignedAt.ValueString()); err != nil {
		t.Errorf("AssignedAt: value %q is not valid RFC3339: %v", model.AssignedAt.ValueString(), err)
	}
}

func TestFlattenRoleAssignment_nullsAssignedAtWhenAbsent(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Attributes.AssignedAt = nil

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if !model.AssignedAt.IsNull() {
		t.Errorf("AssignedAt: expected null, got %q", model.AssignedAt.ValueString())
	}
}

func TestFlattenRoleAssignment_handlesNilAttributes(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Attributes = nil

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "ra-1" {
		t.Errorf("ID: want %q, got %q", "ra-1", got)
	}
}

func TestFlattenRoleAssignment_handlesNilRelationships(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Relationships = nil

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "ra-1" {
		t.Errorf("ID: want %q, got %q", "ra-1", got)
	}
}

func TestFlattenRoleAssignment_nullsRoleIDWhenRelationshipAbsent(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Relationships.Role = nil

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if !model.RoleID.IsNull() {
		t.Errorf("RoleID: expected null, got %q", model.RoleID.ValueString())
	}
}

func TestFlattenRoleAssignment_nullsServiceAccountIDWhenRelationshipAbsent(t *testing.T) {
	fixture := roleAssignmentFlattenFixture()
	fixture.Relationships.ServiceAccount = nil

	var model provider.RoleAssignmentResourceModelForTest
	diags := provider.FlattenRoleAssignmentForTest(fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenRoleAssignmentForTest returned errors: %v", diags)
	}
	if !model.ServiceAccountID.IsNull() {
		t.Errorf("ServiceAccountID: expected null, got %q", model.ServiceAccountID.ValueString())
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

func TestBuildRoleAssignmentRelationships_setsRoleID(t *testing.T) {
	rels := provider.BuildRoleAssignmentRelationships("role-42", "sa-7")
	if got := rels.Role.Data.Id; got != "role-42" {
		t.Errorf("Role.Data.Id: want %q, got %q", "role-42", got)
	}
}

func TestBuildRoleAssignmentRelationships_setsServiceAccountID(t *testing.T) {
	rels := provider.BuildRoleAssignmentRelationships("role-42", "sa-7")
	if got := rels.ServiceAccount.Data.Id; got != "sa-7" {
		t.Errorf("ServiceAccount.Data.Id: want %q, got %q", "sa-7", got)
	}
}

func TestBuildRoleAssignmentRelationships_setsRoleType(t *testing.T) {
	rels := provider.BuildRoleAssignmentRelationships("role-42", "sa-7")
	if got := string(rels.Role.Data.Type); got != "admin.roles" {
		t.Errorf("Role.Data.Type: want %q, got %q", "admin.roles", got)
	}
}

func TestBuildRoleAssignmentRelationships_setsServiceAccountType(t *testing.T) {
	rels := provider.BuildRoleAssignmentRelationships("role-42", "sa-7")
	if got := string(rels.ServiceAccount.Data.Type); got != "admin.serviceAccounts" {
		t.Errorf("ServiceAccount.Data.Type: want %q, got %q", "admin.serviceAccounts", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestRoleAssignmentResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewRoleAssignmentResource()
	if res == nil {
		t.Fatal("NewRoleAssignmentResource returned nil")
	}
}

func TestRoleAssignmentDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewRoleAssignmentDataSource()
	if ds == nil {
		t.Fatal("NewRoleAssignmentDataSource returned nil")
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

func TestAccRoleAssignmentResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → import → destroy).
	// 1. Create a role and a service account.
	// 2. Create a role assignment linking them.
	// 3. Verify assigned_at, role_id, service_account_id round-trip correctly.
	// 4. Verify import by ID restores state without drift.
	// 5. Verify changing role_id or service_account_id triggers replacement.
	// 6. Destroy cleans up the assignment and dependent resources.
}

// TestAccRoleAssignmentDataSource_basic verifies the data source lookup by ID.
func TestAccRoleAssignmentDataSource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test.
	// 1. Create a role assignment via the resource.
	// 2. Look it up via the data source using the role_assignment id.
	// 3. Verify role_id and service_account_id match.
}

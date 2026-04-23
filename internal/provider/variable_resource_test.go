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
// flattenVariable unit tests
// ---------------------------------------------------------------------------

// variableFlattenFixture builds a DataInVariableResponse with all fields populated.
func variableFlattenFixture() api.DataInVariableResponse {
	now := time.Now()
	name := "MY_VAR"
	value := "my-value"
	description := "A test variable"
	monitorID := "monitor-42"

	return api.DataInVariableResponse{
		Id: "variable-1",
		Attributes: &api.AttributesInVariableResponse{
			Name:           &name,
			Value:          &value,
			Description:    &description,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInVariableResponse{
			Monitor: &api.NullableToOneMonitorInResponse{
				Data: &api.MonitorIdentifierInResponse{
					Id: monitorID,
				},
			},
		},
	}
}

func TestFlattenVariable_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Id = "variable-99"

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "variable-99" {
		t.Errorf("ID: want %q, got %q", "variable-99", got)
	}
}

func TestFlattenVariable_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "MY_VAR" {
		t.Errorf("Name: want %q, got %q", "MY_VAR", got)
	}
}

func TestFlattenVariable_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenVariable_setsValueFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.Value.ValueString(); got != "my-value" {
		t.Errorf("Value: want %q, got %q", "my-value", got)
	}
}

func TestFlattenVariable_nullsValueWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Attributes.Value = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.Value.IsNull() {
		t.Errorf("Value: expected null, got %q", model.Value.ValueString())
	}
}

func TestFlattenVariable_setsDescriptionFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.Description.ValueString(); got != "A test variable" {
		t.Errorf("Description: want %q, got %q", "A test variable", got)
	}
}

func TestFlattenVariable_nullsDescriptionWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Attributes.Description = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.Description.IsNull() {
		t.Errorf("Description: expected null, got %q", model.Description.ValueString())
	}
}

func TestFlattenVariable_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenVariable_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenVariable_timestampsAreRFC3339Formatted(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}

	// Verify the timestamp strings are parseable as RFC3339.
	for _, ts := range []struct {
		name  string
		value string
	}{
		{"CreateDateTime", model.CreateDateTime.ValueString()},
		{"UpdateDateTime", model.UpdateDateTime.ValueString()},
	} {
		if _, err := time.Parse(time.RFC3339, ts.value); err != nil {
			t.Errorf("%s: value %q is not valid RFC3339: %v", ts.name, ts.value, err)
		}
	}
}

func TestFlattenVariable_setsMonitorIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.MonitorID.ValueString(); got != "monitor-42" {
		t.Errorf("MonitorID: want %q, got %q", "monitor-42", got)
	}
}

func TestFlattenVariable_nullsMonitorIDWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Relationships = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenVariable_nullsMonitorIDWhenMonitorRelationshipAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Relationships.Monitor = nil

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenVariable_nullsMonitorIDWhenMonitorDataAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := variableFlattenFixture()
	fixture.Relationships.Monitor = &api.NullableToOneMonitorInResponse{
		Data: nil,
	}

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenVariable_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInVariableResponse{
		Id:         "variable-1",
		Attributes: nil,
	}

	var model provider.VariableResourceModelForTest
	diags := provider.FlattenVariableForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenVariableForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "variable-1" {
		t.Errorf("ID: want %q, got %q", "variable-1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestVariableResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewVariableResource()
	if res == nil {
		t.Fatal("NewVariableResource returned nil")
	}
}

func TestVariableDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewVariableDataSource()
	if ds == nil {
		t.Fatal("NewVariableDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildVariablePlanWithMonitorID constructs a minimal variableResourceModel
// with the provided monitor ID.
func buildVariablePlanWithMonitorID(_ context.Context, monitorID string) provider.VariableResourceModelForTest {
	monitorIDVal := types.StringNull()
	if monitorID != "" {
		monitorIDVal = types.StringValue(monitorID)
	}

	return provider.VariableResourceModelForTest{
		Name:      types.StringValue("MY_VAR"),
		Value:     types.StringValue("my-value"),
		MonitorID: monitorIDVal,
	}
}

func TestBuildCreateVariableRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildVariablePlanWithMonitorID(ctx, "monitor-1")

	rels, diags := provider.BuildCreateVariableRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor == nil {
		t.Fatal("Monitor: expected non-nil, got nil")
	}
	if rels.Monitor.Data == nil {
		t.Fatal("Monitor.Data: expected non-nil, got nil")
	}
	if got := rels.Monitor.Data.Id; got != "monitor-1" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "monitor-1", got)
	}
	if got := string(rels.Monitor.Data.Type); got != "monitors" {
		t.Errorf("Monitor.Data.Type: want %q, got %q", "monitors", got)
	}
}

func TestBuildCreateVariableRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildVariablePlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildCreateVariableRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor != nil {
		t.Error("Monitor: expected nil, got non-nil")
	}
}

func TestBuildUpdateVariableRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildVariablePlanWithMonitorID(ctx, "monitor-2")

	rels, diags := provider.BuildUpdateVariableRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor == nil {
		t.Fatal("Monitor: expected non-nil, got nil")
	}
	if rels.Monitor.Data == nil {
		t.Fatal("Monitor.Data: expected non-nil, got nil")
	}
	if got := rels.Monitor.Data.Id; got != "monitor-2" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "monitor-2", got)
	}
	if got := string(rels.Monitor.Data.Type); got != "monitors" {
		t.Errorf("Monitor.Data.Type: want %q, got %q", "monitors", got)
	}
}

func TestBuildUpdateVariableRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildVariablePlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildUpdateVariableRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor != nil {
		t.Error("Monitor: expected nil, got non-nil")
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

func TestAccVariableResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify name, value, description round-trip.
	// Verify monitor_id relationship: create with monitor, confirm monitor_id populated,
	// update value, confirm drift detection works correctly.
}

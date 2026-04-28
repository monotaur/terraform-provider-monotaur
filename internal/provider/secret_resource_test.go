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
// flattenSecret unit tests
// ---------------------------------------------------------------------------

// secretFlattenFixture builds a DataInSecretResponse with all fields populated.
// Note: AttributesInSecretResponse does NOT include a value field — the API
// never returns the secret value on read.
func secretFlattenFixture() api.DataInSecretResponse {
	now := time.Now()
	name := "MY_SECRET"
	description := "A test secret"
	monitorID := "monitor-42"

	return api.DataInSecretResponse{
		Id: "secret-1",
		Attributes: &api.AttributesInSecretResponse{
			Name:           &name,
			Description:    &description,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInSecretResponse{
			Monitor: &api.NullableToOneMonitorInResponse{
				Data: &api.MonitorIdentifierInResponse{
					Id: monitorID,
				},
			},
		},
	}
}

func TestFlattenSecret_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Id = "secret-99"

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "secret-99" {
		t.Errorf("ID: want %q, got %q", "secret-99", got)
	}
}

func TestFlattenSecret_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "MY_SECRET" {
		t.Errorf("Name: want %q, got %q", "MY_SECRET", got)
	}
}

func TestFlattenSecret_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenSecret_setsDescriptionFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.Description.ValueString(); got != "A test secret" {
		t.Errorf("Description: want %q, got %q", "A test secret", got)
	}
}

func TestFlattenSecret_nullsDescriptionWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Attributes.Description = nil

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.Description.IsNull() {
		t.Errorf("Description: expected null, got %q", model.Description.ValueString())
	}
}

func TestFlattenSecret_preservesValueFieldWhenAPIOmitsIt(t *testing.T) {
	// The API never returns value on read. flattenSecret must leave the model's
	// Value field untouched so the prior state value is preserved.
	ctx := context.Background()
	fixture := secretFlattenFixture()

	model := provider.SecretResourceModelForTest{
		Value: types.StringValue("super-secret-value"),
	}
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.Value.ValueString(); got != "super-secret-value" {
		t.Errorf("Value: expected prior state value %q to be preserved, got %q", "super-secret-value", got)
	}
}

func TestFlattenSecret_leavesValueUnchangedWhenModelStartsEmpty(t *testing.T) {
	// When no prior state value exists (e.g. after import), the Value field
	// starts as null/unknown and flattenSecret must not set it to a non-null
	// value (since the API does not return it).
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	// Value should still be null (zero value of types.String).
	if model.Value.ValueString() != "" {
		t.Errorf("Value: expected empty/null when no prior state, got %q", model.Value.ValueString())
	}
}

func TestFlattenSecret_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenSecret_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenSecret_timestampsAreRFC3339Formatted(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
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

func TestFlattenSecret_setsMonitorIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.MonitorID.ValueString(); got != "monitor-42" {
		t.Errorf("MonitorID: want %q, got %q", "monitor-42", got)
	}
}

func TestFlattenSecret_nullsMonitorIDWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Relationships = nil

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenSecret_nullsMonitorIDWhenMonitorRelationshipAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Relationships.Monitor = nil

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenSecret_nullsMonitorIDWhenMonitorDataAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := secretFlattenFixture()
	fixture.Relationships.Monitor = &api.NullableToOneMonitorInResponse{
		Data: nil,
	}

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenSecret_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInSecretResponse{
		Id:         "secret-1",
		Attributes: nil,
	}

	var model provider.SecretResourceModelForTest
	diags := provider.FlattenSecretForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSecretForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "secret-1" {
		t.Errorf("ID: want %q, got %q", "secret-1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestSecretResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewSecretResource()
	if res == nil {
		t.Fatal("NewSecretResource returned nil")
	}
}

func TestSecretDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewSecretDataSource()
	if ds == nil {
		t.Fatal("NewSecretDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildSecretPlanWithMonitorID constructs a minimal secretResourceModel
// with the provided monitor ID.
func buildSecretPlanWithMonitorID(_ context.Context, monitorID string) provider.SecretResourceModelForTest {
	monitorIDVal := types.StringNull()
	if monitorID != "" {
		monitorIDVal = types.StringValue(monitorID)
	}

	return provider.SecretResourceModelForTest{
		Name:      types.StringValue("MY_SECRET"),
		Value:     types.StringValue("super-secret"),
		MonitorID: monitorIDVal,
	}
}

func TestBuildCreateSecretRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildSecretPlanWithMonitorID(ctx, "monitor-1")

	rels, diags := provider.BuildCreateSecretRelationshipsForTest(ctx, plan)
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

func TestBuildCreateSecretRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildSecretPlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch.
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildCreateSecretRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor != nil {
		t.Error("Monitor: expected nil, got non-nil")
	}
}

func TestBuildUpdateSecretRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildSecretPlanWithMonitorID(ctx, "monitor-2")

	rels, diags := provider.BuildUpdateSecretRelationshipsForTest(ctx, plan)
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

func TestBuildUpdateSecretRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildSecretPlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch.
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildUpdateSecretRelationshipsForTest(ctx, plan)
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

func TestAccSecretResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify name, description round-trip.
	// Verify value is marked sensitive and not returned by the API on read.
	// Verify monitor_id relationship: create with monitor, confirm monitor_id populated,
	// update value, confirm drift detection works correctly.
	// Verify UseStateForUnknown preserves value across reads.
}

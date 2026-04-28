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
// flattenMonitor unit tests
// ---------------------------------------------------------------------------

// monitorFlattenFixture builds a DataInMonitorResponse with all fields populated.
func monitorFlattenFixture() api.DataInMonitorResponse {
	name := "my-monitor"
	monType := api.Heartbeat
	status := api.Ok
	statusMsg := "all good"
	now := time.Now()

	return api.DataInMonitorResponse{
		Id: "42",
		Attributes: &api.AttributesInMonitorResponse{
			Name:                     &name,
			Type:                     &monType,
			Status:                   &status,
			StatusMessage:            &statusMsg,
			StatusExpirationDateTime: &now,
			CreateDateTime:           &now,
			UpdateDateTime:           &now,
		},
		Relationships: &api.RelationshipsInMonitorResponse{
			Components: &api.ToManyComponentInResponse{
				Data: &[]api.ComponentIdentifierInResponse{
					{Id: "c1"},
					{Id: "c2"},
				},
			},
		},
	}
}

func TestFlattenMonitor_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()
	fixture.Id = "99"

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "99" {
		t.Errorf("ID: want %q, got %q", "99", got)
	}
}

func TestFlattenMonitor_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "my-monitor" {
		t.Errorf("Name: want %q, got %q", "my-monitor", got)
	}
}

func TestFlattenMonitor_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenMonitor_setsTypeFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if got := model.Type.ValueString(); got != "Heartbeat" {
		t.Errorf("Type: want %q, got %q", "Heartbeat", got)
	}
}

func TestFlattenMonitor_nullsTypeWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()
	fixture.Attributes.Type = nil

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if !model.Type.IsNull() {
		t.Errorf("Type: expected null, got %q", model.Type.ValueString())
	}
}

func TestFlattenMonitor_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenMonitor_setsStatusFields(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if got := model.Status.ValueString(); got != "Ok" {
		t.Errorf("Status: want %q, got %q", "Ok", got)
	}
	if got := model.StatusMessage.ValueString(); got != "all good" {
		t.Errorf("StatusMessage: want %q, got %q", "all good", got)
	}
	if model.StatusExpirationDateTime.IsNull() || model.StatusExpirationDateTime.ValueString() == "" {
		t.Error("StatusExpirationDateTime: expected non-empty value")
	}
}

func TestFlattenMonitor_nullsStatusFieldsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()
	fixture.Attributes.Status = nil
	fixture.Attributes.StatusMessage = nil
	fixture.Attributes.StatusExpirationDateTime = nil

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if !model.Status.IsNull() {
		t.Errorf("Status: expected null, got %q", model.Status.ValueString())
	}
	if !model.StatusMessage.IsNull() {
		t.Errorf("StatusMessage: expected null, got %q", model.StatusMessage.ValueString())
	}
	if !model.StatusExpirationDateTime.IsNull() {
		t.Errorf("StatusExpirationDateTime: expected null, got %q", model.StatusExpirationDateTime.ValueString())
	}
}

func TestFlattenMonitor_setsComponentIDsFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}

	var componentIDs []types.String
	diags = model.ComponentIDs.ElementsAs(ctx, &componentIDs, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}
	if len(componentIDs) != 2 {
		t.Fatalf("ComponentIDs: want 2 items, got %d", len(componentIDs))
	}
	if got := componentIDs[0].ValueString(); got != "c1" {
		t.Errorf("ComponentIDs[0]: want %q, got %q", "c1", got)
	}
	if got := componentIDs[1].ValueString(); got != "c2" {
		t.Errorf("ComponentIDs[1]: want %q, got %q", "c2", got)
	}
}

func TestFlattenMonitor_emptyListsWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := monitorFlattenFixture()
	fixture.Relationships = nil

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if model.ComponentIDs.IsNull() {
		t.Error("ComponentIDs: expected empty list, got null")
	}
	if len(model.ComponentIDs.Elements()) != 0 {
		t.Errorf("ComponentIDs: want 0 elements, got %d", len(model.ComponentIDs.Elements()))
	}
}

func TestFlattenMonitor_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInMonitorResponse{
		Id:         "1",
		Attributes: nil,
	}

	var model provider.MonitorResourceModelForTest
	diags := provider.FlattenMonitorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "1" {
		t.Errorf("ID: want %q, got %q", "1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestMonitorResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewMonitorResource()
	if res == nil {
		t.Fatal("NewMonitorResource returned nil")
	}
}

func TestMonitorDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewMonitorDataSource()
	if ds == nil {
		t.Fatal("NewMonitorDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildMonitorPlanWithComponentIDs constructs a minimal monitorResourceModel with
// the provided component IDs set.
func buildMonitorPlanWithComponentIDs(ctx context.Context, t *testing.T, componentIDs []string) provider.MonitorResourceModelForTest {
	t.Helper()

	toList := func(ids []string) types.List {
		if ids == nil {
			return types.ListNull(types.StringType)
		}
		list, diags := types.ListValueFrom(ctx, types.StringType, ids)
		if diags.HasError() {
			t.Fatalf("types.ListValueFrom error: %v", diags)
		}
		return list
	}

	return provider.MonitorResourceModelForTest{
		Name:         types.StringValue("test-monitor"),
		ComponentIDs: toList(componentIDs),
	}
}

func TestBuildCreateMonitorRelationships_componentIDsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorPlanWithComponentIDs(ctx, t, []string{"c1", "c2"})

	rels, diags := provider.BuildCreateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Components == nil {
		t.Fatal("Components: expected non-nil, got nil")
	}
	if got := len(rels.Components.Data); got != 2 {
		t.Errorf("Components.Data: want 2, got %d", got)
	}
	if got := rels.Components.Data[0].Id; got != "c1" {
		t.Errorf("Components.Data[0].Id: want %q, got %q", "c1", got)
	}
	if got := rels.Components.Data[1].Id; got != "c2" {
		t.Errorf("Components.Data[1].Id: want %q, got %q", "c2", got)
	}
}

func TestBuildCreateMonitorRelationships_nilComponentIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorPlanWithComponentIDs(ctx, t, nil)

	rels, diags := provider.BuildCreateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Components != nil {
		t.Error("Components: expected nil, got non-nil")
	}
}

func TestBuildCreateMonitorRelationships_emptyComponentIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorPlanWithComponentIDs(ctx, t, []string{})

	rels, diags := provider.BuildCreateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Components == nil {
		t.Error("Components: expected non-nil (empty list), got nil")
	} else if got := len(rels.Components.Data); got != 0 {
		t.Errorf("Components.Data: want 0 items, got %d", got)
	}
}

func TestBuildUpdateMonitorRelationships_componentIDsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorPlanWithComponentIDs(ctx, t, []string{"c1"})

	rels, diags := provider.BuildUpdateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Components == nil {
		t.Fatal("Components: expected non-nil, got nil")
	}
	if got := len(rels.Components.Data); got != 1 {
		t.Errorf("Components.Data: want 1, got %d", got)
	}
	if got := rels.Components.Data[0].Id; got != "c1" {
		t.Errorf("Components.Data[0].Id: want %q, got %q", "c1", got)
	}
}

func TestBuildUpdateMonitorRelationships_nilComponentIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorPlanWithComponentIDs(ctx, t, nil)

	rels, diags := provider.BuildUpdateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Components != nil {
		t.Error("Components: expected nil, got non-nil")
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
//   TF_ACC=1 MONOTAUR_API_KEY=<key> MONOTAUR_ENDPOINT=<url> go test ./internal/provider/ -run TestAcc -v

func TestAccMonitorResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify component_ids round-trip: create with component, confirm component_ids populated,
	// update to remove component, confirm drift detection clears component_ids.
}

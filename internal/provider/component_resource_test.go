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
// flattenComponent unit tests
// ---------------------------------------------------------------------------

// componentFlattenFixture builds a DataInComponentResponse with all fields populated.
func componentFlattenFixture() api.DataInComponentResponse {
	name := "my-component"
	now := time.Now()

	return api.DataInComponentResponse{
		Id: "55",
		Attributes: &api.AttributesInComponentResponse{
			Name:           &name,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInComponentResponse{
			Labels: &api.ToManyLabelInResponse{
				Data: &[]api.LabelIdentifierInResponse{
					{Id: "l1"},
					{Id: "l2"},
				},
			},
		},
	}
}

func TestFlattenComponent_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()
	fixture.Id = "99"

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "99" {
		t.Errorf("ID: want %q, got %q", "99", got)
	}
}

func TestFlattenComponent_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "my-component" {
		t.Errorf("Name: want %q, got %q", "my-component", got)
	}
}

func TestFlattenComponent_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenComponent_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenComponent_setsLabelIDsFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}

	var labelIDs []types.String
	diags = model.LabelIDs.ElementsAs(ctx, &labelIDs, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}
	if len(labelIDs) != 2 {
		t.Fatalf("LabelIDs: want 2 items, got %d", len(labelIDs))
	}
	if got := labelIDs[0].ValueString(); got != "l1" {
		t.Errorf("LabelIDs[0]: want %q, got %q", "l1", got)
	}
	if got := labelIDs[1].ValueString(); got != "l2" {
		t.Errorf("LabelIDs[1]: want %q, got %q", "l2", got)
	}
}

func TestFlattenComponent_emptyListsWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()
	fixture.Relationships = nil

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if model.LabelIDs.IsNull() {
		t.Error("LabelIDs: expected empty list, got null")
	}
	if len(model.LabelIDs.Elements()) != 0 {
		t.Errorf("LabelIDs: want 0 elements, got %d", len(model.LabelIDs.Elements()))
	}
	if model.MaintenanceWindowIDs.IsNull() {
		t.Error("MaintenanceWindowIDs: expected empty list, got null")
	}
	if model.MonitorIDs.IsNull() {
		t.Error("MonitorIDs: expected empty list, got null")
	}
	if !model.BusinessHoursID.IsNull() {
		t.Errorf("BusinessHoursID: expected null when no relationships, got %q", model.BusinessHoursID.ValueString())
	}
}

func TestFlattenComponent_setsAllRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()
	bhID := "bh1"
	fixture.Relationships = &api.RelationshipsInComponentResponse{
		BusinessHours: &api.NullableToOneBusinessHourInResponse{
			Data: &api.BusinessHourIdentifierInResponse{Id: bhID},
		},
		Labels: &api.ToManyLabelInResponse{
			Data: &[]api.LabelIdentifierInResponse{
				{Id: "l1"},
			},
		},
		MaintenanceWindows: &api.ToManyMaintenanceWindowInResponse{
			Data: &[]api.MaintenanceWindowIdentifierInResponse{
				{Id: "mw1"},
			},
		},
		Monitors: &api.ToManyMonitorInResponse{
			Data: &[]api.MonitorIdentifierInResponse{
				{Id: "m1"},
			},
		},
	}

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}

	if got := model.BusinessHoursID.ValueString(); got != "bh1" {
		t.Errorf("BusinessHoursID: want %q, got %q", "bh1", got)
	}

	checkSingleID := func(name string, list types.List, want string) {
		t.Helper()
		var ids []types.String
		if d := list.ElementsAs(ctx, &ids, false); d.HasError() {
			t.Fatalf("%s ElementsAs error: %v", name, d)
		}
		if len(ids) != 1 {
			t.Fatalf("%s: want 1 element, got %d", name, len(ids))
		}
		if got := ids[0].ValueString(); got != want {
			t.Errorf("%s: want %q, got %q", name, want, got)
		}
	}

	checkSingleID("LabelIDs", model.LabelIDs, "l1")
	checkSingleID("MaintenanceWindowIDs", model.MaintenanceWindowIDs, "mw1")
	checkSingleID("MonitorIDs", model.MonitorIDs, "m1")
}

func TestFlattenComponent_nullBusinessHoursWhenDataIsNil(t *testing.T) {
	ctx := context.Background()
	fixture := componentFlattenFixture()
	fixture.Relationships = &api.RelationshipsInComponentResponse{
		BusinessHours: &api.NullableToOneBusinessHourInResponse{
			Data: nil,
		},
	}

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if !model.BusinessHoursID.IsNull() {
		t.Errorf("BusinessHoursID: expected null when Data is nil, got %q", model.BusinessHoursID.ValueString())
	}
}

func TestFlattenComponent_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInComponentResponse{
		Id:         "1",
		Attributes: nil,
	}

	var model provider.ComponentResourceModelForTest
	diags := provider.FlattenComponentForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenComponentForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "1" {
		t.Errorf("ID: want %q, got %q", "1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestComponentResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewComponentResource()
	if res == nil {
		t.Fatal("NewComponentResource returned nil")
	}
}

func TestComponentDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewComponentDataSource()
	if ds == nil {
		t.Fatal("NewComponentDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildComponentPlanWithIDs constructs a minimal componentResourceModel with the provided
// relationship ID fields set. Pass "" for businessHoursID to leave it null.
func buildComponentPlanWithIDs(ctx context.Context, t *testing.T, businessHoursID string, labelIDs, maintenanceWindowIDs, monitorIDs []string) provider.ComponentResourceModelForTest {
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

	var bhID types.String
	if businessHoursID == "" {
		bhID = types.StringNull()
	} else {
		bhID = types.StringValue(businessHoursID)
	}

	return provider.ComponentResourceModelForTest{
		Name:                 types.StringValue("test-component"),
		BusinessHoursID:      bhID,
		LabelIDs:             toList(labelIDs),
		MaintenanceWindowIDs: toList(maintenanceWindowIDs),
		MonitorIDs:           toList(monitorIDs),
	}
}

func TestBuildCreateComponentRelationships_labelsOnly(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "", []string{"l1", "l2"}, nil, nil)

	rels, diags := provider.BuildCreateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.BusinessHours != nil {
		t.Error("BusinessHours: expected nil, got non-nil")
	}
	if rels.MaintenanceWindows != nil {
		t.Error("MaintenanceWindows: expected nil, got non-nil")
	}
	if rels.Monitors != nil {
		t.Error("Monitors: expected nil, got non-nil")
	}
	if rels.Labels == nil {
		t.Fatal("Labels: expected non-nil, got nil")
	}
	if got := len(rels.Labels.Data); got != 2 {
		t.Errorf("Labels.Data: want 2, got %d", got)
	}
	if got := rels.Labels.Data[0].Id; got != "l1" {
		t.Errorf("Labels.Data[0].Id: want %q, got %q", "l1", got)
	}
}

func TestBuildCreateComponentRelationships_businessHoursSet(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "bh-123", nil, nil, nil)

	rels, diags := provider.BuildCreateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.BusinessHours == nil {
		t.Fatal("BusinessHours: expected non-nil, got nil")
	}
	if rels.BusinessHours.Data == nil {
		t.Fatal("BusinessHours.Data: expected non-nil, got nil")
	}
	if got := rels.BusinessHours.Data.Id; got != "bh-123" {
		t.Errorf("BusinessHours.Data.Id: want %q, got %q", "bh-123", got)
	}
}

func TestBuildCreateComponentRelationships_allRelationshipsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "bh1", []string{"l1"}, []string{"mw1"}, []string{"m1"})

	rels, diags := provider.BuildCreateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.BusinessHours == nil || rels.BusinessHours.Data == nil {
		t.Errorf("BusinessHours: want non-nil, got %v", rels.BusinessHours)
	}
	if rels.Labels == nil || len(rels.Labels.Data) != 1 {
		t.Errorf("Labels: want 1 item, got %v", rels.Labels)
	}
	if rels.MaintenanceWindows == nil || len(rels.MaintenanceWindows.Data) != 1 {
		t.Errorf("MaintenanceWindows: want 1 item, got %v", rels.MaintenanceWindows)
	}
	if rels.Monitors == nil || len(rels.Monitors.Data) != 1 {
		t.Errorf("Monitors: want 1 item, got %v", rels.Monitors)
	}
}

func TestBuildCreateComponentRelationships_emptyLists(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "", []string{}, []string{}, []string{})

	rels, diags := provider.BuildCreateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Labels == nil {
		t.Error("Labels: expected non-nil (empty list), got nil")
	} else if got := len(rels.Labels.Data); got != 0 {
		t.Errorf("Labels.Data: want 0 items, got %d", got)
	}
	if rels.MaintenanceWindows == nil {
		t.Error("MaintenanceWindows: expected non-nil (empty list), got nil")
	} else if got := len(rels.MaintenanceWindows.Data); got != 0 {
		t.Errorf("MaintenanceWindows.Data: want 0 items, got %d", got)
	}
	if rels.Monitors == nil {
		t.Error("Monitors: expected non-nil (empty list), got nil")
	} else if got := len(rels.Monitors.Data); got != 0 {
		t.Errorf("Monitors.Data: want 0 items, got %d", got)
	}
}

func TestBuildUpdateComponentRelationships_labelsOnly(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "", nil, nil, []string{"m1", "m2"})

	rels, diags := provider.BuildUpdateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Labels != nil {
		t.Error("Labels: expected nil, got non-nil")
	}
	if rels.MaintenanceWindows != nil {
		t.Error("MaintenanceWindows: expected nil, got non-nil")
	}
	if rels.Monitors == nil {
		t.Fatal("Monitors: expected non-nil, got nil")
	}
	if got := len(rels.Monitors.Data); got != 2 {
		t.Errorf("Monitors.Data: want 2, got %d", got)
	}
	if got := rels.Monitors.Data[0].Id; got != "m1" {
		t.Errorf("Monitors.Data[0].Id: want %q, got %q", "m1", got)
	}
}

func TestBuildUpdateComponentRelationships_allPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildComponentPlanWithIDs(ctx, t, "bh1", []string{"l1"}, []string{"mw1"}, []string{"m1"})

	rels, diags := provider.BuildUpdateComponentRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.BusinessHours == nil || rels.BusinessHours.Data == nil {
		t.Errorf("BusinessHours: want non-nil, got %v", rels.BusinessHours)
	}
	if rels.Labels == nil || len(rels.Labels.Data) != 1 {
		t.Errorf("Labels: want 1 item, got %v", rels.Labels)
	}
	if rels.MaintenanceWindows == nil || len(rels.MaintenanceWindows.Data) != 1 {
		t.Errorf("MaintenanceWindows: want 1 item, got %v", rels.MaintenanceWindows)
	}
	if rels.Monitors == nil || len(rels.Monitors.Data) != 1 {
		t.Errorf("Monitors: want 1 item, got %v", rels.Monitors)
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

func TestAccComponentResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify label_ids round-trip: create with label, confirm label_ids populated,
	// update to remove label, confirm drift detection clears label_ids.
}

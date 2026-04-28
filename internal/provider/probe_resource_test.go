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
// flattenProbe unit tests
// ---------------------------------------------------------------------------

// probeFlattenFixture builds a DataInProbeResponse with all fields populated.
func probeFlattenFixture() api.DataInProbeResponse {
	active := true
	schedule := "*/5 * * * *"
	now := time.Now()

	return api.DataInProbeResponse{
		Id: "42",
		Attributes: &api.AttributesInProbeResponse{
			Active:         &active,
			Schedule:       &schedule,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInProbeResponse{
			Monitor: &api.ToOneMonitorInResponse{
				Data: &api.MonitorIdentifierInResponse{Id: "m1"},
			},
			Sensors: &api.ToManySensorInResponse{
				Data: &[]api.SensorIdentifierInResponse{
					{Id: "s1"},
					{Id: "s2"},
				},
			},
		},
	}
}

func TestFlattenProbe_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Id = "99"

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "99" {
		t.Errorf("ID: want %q, got %q", "99", got)
	}
}

func TestFlattenProbe_setsActiveFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if !model.Active.ValueBool() {
		t.Error("Active: want true, got false")
	}
}

func TestFlattenProbe_nullsActiveWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Attributes.Active = nil

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if !model.Active.IsNull() {
		t.Errorf("Active: expected null, got %v", model.Active.ValueBool())
	}
}

func TestFlattenProbe_setsScheduleFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if got := model.Schedule.ValueString(); got != "*/5 * * * *" {
		t.Errorf("Schedule: want %q, got %q", "*/5 * * * *", got)
	}
}

func TestFlattenProbe_nullsScheduleWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Attributes.Schedule = nil

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if !model.Schedule.IsNull() {
		t.Errorf("Schedule: expected null, got %q", model.Schedule.ValueString())
	}
}

func TestFlattenProbe_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenProbe_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenProbe_setsMonitorIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if got := model.MonitorID.ValueString(); got != "m1" {
		t.Errorf("MonitorID: want %q, got %q", "m1", got)
	}
}

func TestFlattenProbe_nullsMonitorIDWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Relationships.Monitor = nil

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenProbe_setsSensorIDsFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}

	var sensorIDs []types.String
	diags = model.SensorIDs.ElementsAs(ctx, &sensorIDs, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}
	if len(sensorIDs) != 2 {
		t.Fatalf("SensorIDs: want 2 items, got %d", len(sensorIDs))
	}
	if got := sensorIDs[0].ValueString(); got != "s1" {
		t.Errorf("SensorIDs[0]: want %q, got %q", "s1", got)
	}
	if got := sensorIDs[1].ValueString(); got != "s2" {
		t.Errorf("SensorIDs[1]: want %q, got %q", "s2", got)
	}
}

func TestFlattenProbe_emptyListsWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := probeFlattenFixture()
	fixture.Relationships = nil

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if model.SensorIDs.IsNull() {
		t.Error("SensorIDs: expected empty list, got null")
	}
	if len(model.SensorIDs.Elements()) != 0 {
		t.Errorf("SensorIDs: want 0 elements, got %d", len(model.SensorIDs.Elements()))
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null when no relationships, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenProbe_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInProbeResponse{
		Id:         "1",
		Attributes: nil,
	}

	var model provider.ProbeResourceModelForTest
	diags := provider.FlattenProbeForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenProbeForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "1" {
		t.Errorf("ID: want %q, got %q", "1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestProbeResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewProbeResource()
	if res == nil {
		t.Fatal("NewProbeResource returned nil")
	}
}

func TestProbeDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewProbeDataSource()
	if ds == nil {
		t.Fatal("NewProbeDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildProbePlanWithSensorIDs constructs a minimal probeResourceModel with
// the provided sensor IDs set and the required monitor_id.
func buildProbePlanWithSensorIDs(ctx context.Context, t *testing.T, monitorID string, sensorIDs []string) provider.ProbeResourceModelForTest {
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

	return provider.ProbeResourceModelForTest{
		MonitorID: types.StringValue(monitorID),
		SensorIDs: toList(sensorIDs),
	}
}

func TestBuildCreateProbeRelationships_monitorIDSet(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", nil)

	rels, diags := provider.BuildCreateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := rels.Monitor.Data.Id; got != "mon-1" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "mon-1", got)
	}
}

func TestBuildCreateProbeRelationships_sensorIDsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", []string{"s1", "s2"})

	rels, diags := provider.BuildCreateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Sensors == nil {
		t.Fatal("Sensors: expected non-nil, got nil")
	}
	if got := len(rels.Sensors.Data); got != 2 {
		t.Errorf("Sensors.Data: want 2, got %d", got)
	}
	if got := rels.Sensors.Data[0].Id; got != "s1" {
		t.Errorf("Sensors.Data[0].Id: want %q, got %q", "s1", got)
	}
	if got := rels.Sensors.Data[1].Id; got != "s2" {
		t.Errorf("Sensors.Data[1].Id: want %q, got %q", "s2", got)
	}
}

func TestBuildCreateProbeRelationships_nilSensorIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", nil)

	rels, diags := provider.BuildCreateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Sensors != nil {
		t.Error("Sensors: expected nil, got non-nil")
	}
}

func TestBuildCreateProbeRelationships_emptySensorIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", []string{})

	rels, diags := provider.BuildCreateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Sensors == nil {
		t.Error("Sensors: expected non-nil (empty list), got nil")
	} else if got := len(rels.Sensors.Data); got != 0 {
		t.Errorf("Sensors.Data: want 0 items, got %d", got)
	}
}

func TestBuildUpdateProbeRelationships_sensorIDsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", []string{"s1"})

	rels, diags := provider.BuildUpdateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Sensors == nil {
		t.Fatal("Sensors: expected non-nil, got nil")
	}
	if got := len(rels.Sensors.Data); got != 1 {
		t.Errorf("Sensors.Data: want 1, got %d", got)
	}
	if got := rels.Sensors.Data[0].Id; got != "s1" {
		t.Errorf("Sensors.Data[0].Id: want %q, got %q", "s1", got)
	}
}

func TestBuildUpdateProbeRelationships_nilSensorIDs(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-1", nil)

	rels, diags := provider.BuildUpdateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Sensors != nil {
		t.Error("Sensors: expected nil, got non-nil")
	}
}

func TestBuildUpdateProbeRelationships_monitorIDSet(t *testing.T) {
	ctx := context.Background()
	plan := buildProbePlanWithSensorIDs(ctx, t, "mon-2", nil)

	rels, diags := provider.BuildUpdateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor == nil {
		t.Fatal("Monitor: expected non-nil, got nil")
	}
	if got := rels.Monitor.Data.Id; got != "mon-2" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "mon-2", got)
	}
}

func TestBuildUpdateProbeRelationships_nilMonitorID(t *testing.T) {
	ctx := context.Background()
	plan := provider.ProbeResourceModelForTest{
		MonitorID: types.StringNull(),
		SensorIDs: types.ListNull(types.StringType),
	}

	rels, diags := provider.BuildUpdateProbeRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor != nil {
		t.Error("Monitor: expected nil when MonitorID is null, got non-nil")
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

func TestAccProbeResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify sensor_ids round-trip: create with sensor, confirm sensor_ids populated,
	// update to remove sensor, confirm drift detection clears sensor_ids.
}

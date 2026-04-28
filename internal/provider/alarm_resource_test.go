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
// flattenAlarm unit tests
// ---------------------------------------------------------------------------

// alarmFlattenFixture builds a DataInAlarmResponse with all fields populated.
func alarmFlattenFixture() api.DataInAlarmResponse {
	now := time.Now()
	start := now.Add(-2 * time.Hour)
	end := now.Add(-1 * time.Hour)
	excludeFromDowntime := true
	squelch := false
	monitorID := "monitor-7"

	return api.DataInAlarmResponse{
		Id: "alarm-1",
		Attributes: &api.AttributesInAlarmResponse{
			StartDateTime:       &start,
			EndDateTime:         &end,
			ExcludeFromDowntime: &excludeFromDowntime,
			Squelch:             &squelch,
			CreateDateTime:      &now,
			UpdateDateTime:      &now,
		},
		Relationships: &api.RelationshipsInAlarmResponse{
			Monitor: &api.ToOneMonitorInResponse{
				Data: &api.MonitorIdentifierInResponse{
					Id: monitorID,
				},
			},
		},
	}
}

func TestFlattenAlarm_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Id = "alarm-99"

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "alarm-99" {
		t.Errorf("ID: want %q, got %q", "alarm-99", got)
	}
}

func TestFlattenAlarm_setsStartDateTimeFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if model.StartDateTime.IsNull() || model.StartDateTime.ValueString() == "" {
		t.Error("StartDateTime: expected non-empty value")
	}
}

func TestFlattenAlarm_nullsStartDateTimeWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Attributes.StartDateTime = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.StartDateTime.IsNull() {
		t.Errorf("StartDateTime: expected null, got %q", model.StartDateTime.ValueString())
	}
}

func TestFlattenAlarm_setsEndDateTimeFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if model.EndDateTime.IsNull() || model.EndDateTime.ValueString() == "" {
		t.Error("EndDateTime: expected non-empty value")
	}
}

func TestFlattenAlarm_nullsEndDateTimeWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Attributes.EndDateTime = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.EndDateTime.IsNull() {
		t.Errorf("EndDateTime: expected null, got %q", model.EndDateTime.ValueString())
	}
}

func TestFlattenAlarm_setsExcludeFromDowntimeFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if model.ExcludeFromDowntime.IsNull() {
		t.Error("ExcludeFromDowntime: expected non-null value")
	}
	if !model.ExcludeFromDowntime.ValueBool() {
		t.Errorf("ExcludeFromDowntime: want true, got false")
	}
}

func TestFlattenAlarm_nullsExcludeFromDowntimeWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Attributes.ExcludeFromDowntime = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.ExcludeFromDowntime.IsNull() {
		t.Errorf("ExcludeFromDowntime: expected null, got %v", model.ExcludeFromDowntime.ValueBool())
	}
}

func TestFlattenAlarm_setsSquelchFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if model.Squelch.IsNull() {
		t.Error("Squelch: expected non-null value")
	}
	if model.Squelch.ValueBool() {
		t.Errorf("Squelch: want false, got true")
	}
}

func TestFlattenAlarm_nullsSquelchWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Attributes.Squelch = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.Squelch.IsNull() {
		t.Errorf("Squelch: expected null, got %v", model.Squelch.ValueBool())
	}
}

func TestFlattenAlarm_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenAlarm_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenAlarm_setsMonitorIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if got := model.MonitorID.ValueString(); got != "monitor-7" {
		t.Errorf("MonitorID: want %q, got %q", "monitor-7", got)
	}
}

func TestFlattenAlarm_nullsMonitorIDWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Relationships = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenAlarm_nullsMonitorIDWhenMonitorRelationshipAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()
	fixture.Relationships.Monitor = nil

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenAlarm_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInAlarmResponse{
		Id:         "alarm-1",
		Attributes: nil,
	}

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "alarm-1" {
		t.Errorf("ID: want %q, got %q", "alarm-1", got)
	}
}

func TestFlattenAlarm_timestampsAreRFC3339Formatted(t *testing.T) {
	ctx := context.Background()
	fixture := alarmFlattenFixture()

	var model provider.AlarmResourceModelForTest
	diags := provider.FlattenAlarmForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenAlarmForTest returned errors: %v", diags)
	}

	// Verify the timestamp strings are parseable as RFC3339.
	for _, ts := range []struct {
		name  string
		value string
	}{
		{"CreateDateTime", model.CreateDateTime.ValueString()},
		{"UpdateDateTime", model.UpdateDateTime.ValueString()},
		{"StartDateTime", model.StartDateTime.ValueString()},
		{"EndDateTime", model.EndDateTime.ValueString()},
	} {
		if _, err := time.Parse(time.RFC3339, ts.value); err != nil {
			t.Errorf("%s: value %q is not valid RFC3339: %v", ts.name, ts.value, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestAlarmResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewAlarmResource()
	if res == nil {
		t.Fatal("NewAlarmResource returned nil")
	}
}

func TestAlarmDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewAlarmDataSource()
	if ds == nil {
		t.Fatal("NewAlarmDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildAlarmPlanWithMonitorID constructs a minimal alarmResourceModel
// with the provided monitor ID.
func buildAlarmPlanWithMonitorID(_ context.Context, monitorID string) provider.AlarmResourceModelForTest {
	monitorIDVal := types.StringNull()
	if monitorID != "" {
		monitorIDVal = types.StringValue(monitorID)
	}

	return provider.AlarmResourceModelForTest{
		MonitorID: monitorIDVal,
	}
}

func TestBuildCreateAlarmRelationships_setsMonitorID(t *testing.T) {
	ctx := context.Background()
	plan := buildAlarmPlanWithMonitorID(ctx, "monitor-1")

	rels, diags := provider.BuildCreateAlarmRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := rels.Monitor.Data.Id; got != "monitor-1" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "monitor-1", got)
	}
	if got := string(rels.Monitor.Data.Type); got != "monitors" {
		t.Errorf("Monitor.Data.Type: want %q, got %q", "monitors", got)
	}
}

func TestBuildCreateAlarmRelationships_emptyMonitorID(t *testing.T) {
	ctx := context.Background()
	plan := buildAlarmPlanWithMonitorID(ctx, "")

	rels, diags := provider.BuildCreateAlarmRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	// Even with an empty string, the monitor relationship struct is set (create always requires it)
	if got := rels.Monitor.Data.Id; got != "" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "", got)
	}
}

func TestBuildUpdateAlarmRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildAlarmPlanWithMonitorID(ctx, "monitor-2")

	rels, diags := provider.BuildUpdateAlarmRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Monitor == nil {
		t.Fatal("Monitor: expected non-nil, got nil")
	}
	if got := rels.Monitor.Data.Id; got != "monitor-2" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "monitor-2", got)
	}
	if got := string(rels.Monitor.Data.Type); got != "monitors" {
		t.Errorf("Monitor.Data.Type: want %q, got %q", "monitors", got)
	}
}

func TestBuildUpdateAlarmRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildAlarmPlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildUpdateAlarmRelationshipsForTest(ctx, plan)
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

func TestAccAlarmResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify monitor_id round-trip: create with monitor, confirm monitor_id populated,
	// update squelch/excludeFromDowntime, confirm drift detection works correctly.
}

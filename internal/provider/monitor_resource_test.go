package provider_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	status := api.MonitorStatusOk
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

func TestFlattenMonitor_setsSettings(t *testing.T) {
	enabled := false
	interval := "00:05:00"
	grace := "00:00:30"
	noData := api.MonitorStatusFault
	missed := int32(5)
	fault := int32(3)
	recovery := int32(2)
	delay := "00:02:00"
	flapping := int32(6)
	pending := api.MonitorStatusFault
	pendingCount := int32(1)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	data := api.DataInMonitorResponse{
		Id: "1",
		Attributes: &api.AttributesInMonitorResponse{
			Enabled:                &enabled,
			HeartbeatInterval:      &interval,
			HeartbeatGrace:         &grace,
			NoDataStatus:           &noData,
			NoDataMissedIntervals:  &missed,
			NoDataDeadlineDateTime: &now,
			NoDataDateTime:         &now,
			FaultThreshold:         &fault,
			RecoveryThreshold:      &recovery,
			ConfirmationDelay:      &delay,
			PendingStatus:          &pending,
			PendingStatusCount:     &pendingCount,
			PendingStatusDateTime:  &now,
			FlappingThreshold:      &flapping,
			FlappingDateTime:       &now,
		},
		Relationships: &api.RelationshipsInMonitorResponse{
			Labels: &api.ToManyLabelInResponse{Data: &[]api.LabelIdentifierInResponse{{Id: "7", Type: api.Labels}}},
		},
	}

	var model provider.MonitorResourceModelForTest
	if diags := provider.FlattenMonitorForTest(context.Background(), data, &model); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	ts := now.Format(time.RFC3339)
	checks := map[string]struct{ got, want string }{
		"enabled":                    {model.Enabled.String(), "false"},
		"heartbeat_interval":         {model.HeartbeatInterval.ValueString(), interval},
		"heartbeat_grace":            {model.HeartbeatGrace.ValueString(), grace},
		"no_data_status":             {model.NoDataStatus.ValueString(), "Fault"},
		"no_data_missed_intervals":   {model.NoDataMissedIntervals.String(), "5"},
		"no_data_deadline_date_time": {model.NoDataDeadlineDateTime.ValueString(), ts},
		"no_data_date_time":          {model.NoDataDateTime.ValueString(), ts},
		"fault_threshold":            {model.FaultThreshold.String(), "3"},
		"recovery_threshold":         {model.RecoveryThreshold.String(), "2"},
		"confirmation_delay":         {model.ConfirmationDelay.ValueString(), delay},
		"pending_status":             {model.PendingStatus.ValueString(), "Fault"},
		"pending_status_count":       {model.PendingStatusCount.String(), "1"},
		"pending_status_date_time":   {model.PendingStatusDateTime.ValueString(), ts},
		"flapping_threshold":         {model.FlappingThreshold.String(), "6"},
		"flapping_date_time":         {model.FlappingDateTime.ValueString(), ts},
		"label_ids":                  {model.LabelIDs.String(), `["7"]`},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", name, c.got, c.want)
		}
	}
}

func TestFlattenMonitor_nullsSettingsWhenAbsent(t *testing.T) {
	model := provider.MonitorResourceModelForTest{
		HeartbeatInterval: types.StringValue("00:05:00"),
		FlappingThreshold: types.Int64Value(4),
	}
	data := api.DataInMonitorResponse{Id: "1", Attributes: &api.AttributesInMonitorResponse{}}
	if diags := provider.FlattenMonitorForTest(context.Background(), data, &model); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for name, v := range map[string]interface{ IsNull() bool }{
		"enabled":            model.Enabled,
		"heartbeat_interval": model.HeartbeatInterval,
		"no_data_status":     model.NoDataStatus,
		"flapping_threshold": model.FlappingThreshold,
		"flapping_date_time": model.FlappingDateTime,
	} {
		if !v.IsNull() {
			t.Errorf("%s should be null", name)
		}
	}
	if model.LabelIDs.IsNull() || len(model.LabelIDs.Elements()) != 0 {
		t.Errorf("label_ids = %v, want empty list", model.LabelIDs)
	}
}

func TestExpandMonitorSettings_setValues(t *testing.T) {
	plan := provider.MonitorResourceModelForTest{
		Enabled:               types.BoolValue(false),
		HeartbeatInterval:     types.StringValue("00:05:00"),
		HeartbeatGrace:        types.StringValue("00:00:30"),
		NoDataStatus:          types.StringValue("Fault"),
		NoDataMissedIntervals: types.Int64Value(0),
		FaultThreshold:        types.Int64Value(3),
		RecoveryThreshold:     types.Int64Value(2),
		ConfirmationDelay:     types.StringValue("00:02:00"),
		FlappingThreshold:     types.Int64Value(6),
	}
	s := provider.ExpandMonitorSettingsForTest(plan)
	if s.Enabled == nil || *s.Enabled {
		t.Errorf("Enabled = %v, want false", s.Enabled)
	}
	if s.HeartbeatInterval == nil || *s.HeartbeatInterval != "00:05:00" {
		t.Errorf("HeartbeatInterval = %v", s.HeartbeatInterval)
	}
	if s.NoDataStatus == nil || *s.NoDataStatus != api.MonitorStatusFault {
		t.Errorf("NoDataStatus = %v", s.NoDataStatus)
	}
	if s.NoDataMissedIntervals == nil || *s.NoDataMissedIntervals != 0 {
		t.Errorf("NoDataMissedIntervals = %v, want 0", s.NoDataMissedIntervals)
	}
	if s.FaultThreshold == nil || *s.FaultThreshold != 3 || s.RecoveryThreshold == nil || *s.RecoveryThreshold != 2 {
		t.Errorf("thresholds = %v/%v", s.FaultThreshold, s.RecoveryThreshold)
	}
	if s.FlappingThreshold == nil || *s.FlappingThreshold != 6 {
		t.Errorf("FlappingThreshold = %v", s.FlappingThreshold)
	}
	if len(s.Nulls) != 0 {
		t.Errorf("Nulls = %v, want none", s.Nulls)
	}
}

func TestExpandMonitorSettings_nullsUnsetNullableAttributes(t *testing.T) {
	plan := provider.MonitorResourceModelForTest{
		Enabled:               types.BoolUnknown(),
		HeartbeatInterval:     types.StringNull(),
		HeartbeatGrace:        types.StringNull(),
		NoDataStatus:          types.StringNull(),
		NoDataMissedIntervals: types.Int64Null(),
		FaultThreshold:        types.Int64Unknown(),
		RecoveryThreshold:     types.Int64Unknown(),
		ConfirmationDelay:     types.StringNull(),
		FlappingThreshold:     types.Int64Null(),
	}
	s := provider.ExpandMonitorSettingsForTest(plan)
	if s.Enabled != nil || s.FaultThreshold != nil || s.RecoveryThreshold != nil {
		t.Error("unknown Optional+Computed attributes must be omitted, not nulled")
	}
	want := []string{"heartbeatInterval", "heartbeatGrace", "noDataStatus", "noDataMissedIntervals", "confirmationDelay", "flappingThreshold"}
	if len(s.Nulls) != len(want) {
		t.Fatalf("Nulls = %v, want %v", s.Nulls, want)
	}
	for i := range want {
		if s.Nulls[i] != want[i] {
			t.Errorf("Nulls[%d] = %q, want %q", i, s.Nulls[i], want[i])
		}
	}
}

func TestBuildCreateMonitorRelationships_labelIDsPopulated(t *testing.T) {
	ctx := context.Background()
	plan := provider.MonitorResourceModelForTest{
		ComponentIDs: types.ListNull(types.StringType),
		LabelIDs:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("3"), types.StringValue("4")}),
	}
	rels, diags := provider.BuildCreateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Labels == nil || len(rels.Labels.Data) != 2 || rels.Labels.Data[1].Id != "4" || rels.Labels.Data[0].Type != api.ResourceTypeLabels {
		t.Errorf("Labels = %+v", rels.Labels)
	}
	if rels.Components != nil {
		t.Error("Components should be omitted when component_ids is null")
	}
}

func TestBuildUpdateMonitorRelationships_emptyLabelIDsClears(t *testing.T) {
	ctx := context.Background()
	plan := provider.MonitorResourceModelForTest{
		ComponentIDs: types.ListNull(types.StringType),
		LabelIDs:     types.ListValueMust(types.StringType, nil),
	}
	rels, diags := provider.BuildUpdateMonitorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Labels == nil || len(rels.Labels.Data) != 0 {
		t.Errorf("Labels = %+v, want empty data", rels.Labels)
	}
}

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

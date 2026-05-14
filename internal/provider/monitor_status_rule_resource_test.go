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
// flattenMonitorStatusRule unit tests
// ---------------------------------------------------------------------------

// monitorStatusRuleFlattenFixture builds a DataInMonitorStatusRuleResponse with all fields populated.
func monitorStatusRuleFlattenFixture() api.DataInMonitorStatusRuleResponse {
	predicate := "probe.status == 'Fault'"
	status := api.MonitorStatus("Fault")
	statusMsg := "probe is down"
	now := time.Now()
	monitorID := "monitor-42"

	return api.DataInMonitorStatusRuleResponse{
		Id: "rule-1",
		Attributes: &api.AttributesInMonitorStatusRuleResponse{
			Predicate:      &predicate,
			Status:         &status,
			StatusMessage:  &statusMsg,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInMonitorStatusRuleResponse{
			Monitor: &api.ToOneMonitorInResponse{
				Data: &api.MonitorIdentifierInResponse{
					Id: monitorID,
				},
			},
		},
	}
}

func TestFlattenMonitorStatusRule_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Id = "rule-99"

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "rule-99" {
		t.Errorf("ID: want %q, got %q", "rule-99", got)
	}
}

func TestFlattenMonitorStatusRule_setsPredicateFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.Predicate.ValueString(); got != "probe.status == 'Fault'" {
		t.Errorf("Predicate: want %q, got %q", "probe.status == 'Fault'", got)
	}
}

func TestFlattenMonitorStatusRule_nullsPredicateWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Attributes.Predicate = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.Predicate.IsNull() {
		t.Errorf("Predicate: expected null, got %q", model.Predicate.ValueString())
	}
}

func TestFlattenMonitorStatusRule_setsStatusFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.Status.ValueString(); got != "Fault" {
		t.Errorf("Status: want %q, got %q", "Fault", got)
	}
}

func TestFlattenMonitorStatusRule_nullsStatusWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Attributes.Status = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.Status.IsNull() {
		t.Errorf("Status: expected null, got %q", model.Status.ValueString())
	}
}

func TestFlattenMonitorStatusRule_setsStatusMessageFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.StatusMessage.ValueString(); got != "probe is down" {
		t.Errorf("StatusMessage: want %q, got %q", "probe is down", got)
	}
}

func TestFlattenMonitorStatusRule_nullsStatusMessageWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Attributes.StatusMessage = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.StatusMessage.IsNull() {
		t.Errorf("StatusMessage: expected null, got %q", model.StatusMessage.ValueString())
	}
}

func TestFlattenMonitorStatusRule_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenMonitorStatusRule_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenMonitorStatusRule_setsMonitorIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.MonitorID.ValueString(); got != "monitor-42" {
		t.Errorf("MonitorID: want %q, got %q", "monitor-42", got)
	}
}

func TestFlattenMonitorStatusRule_nullsMonitorIDWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Relationships = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenMonitorStatusRule_nullsMonitorIDWhenMonitorRelationshipAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := monitorStatusRuleFlattenFixture()
	fixture.Relationships.Monitor = nil

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID: expected null, got %q", model.MonitorID.ValueString())
	}
}

func TestFlattenMonitorStatusRule_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInMonitorStatusRuleResponse{
		Id:         "rule-1",
		Attributes: nil,
	}

	var model provider.MonitorStatusRuleResourceModelForTest
	diags := provider.FlattenMonitorStatusRuleForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenMonitorStatusRuleForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "rule-1" {
		t.Errorf("ID: want %q, got %q", "rule-1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestMonitorStatusRuleResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewMonitorStatusRuleResource()
	if res == nil {
		t.Fatal("NewMonitorStatusRuleResource returned nil")
	}
}

func TestMonitorStatusRuleDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewMonitorStatusRuleDataSource()
	if ds == nil {
		t.Fatal("NewMonitorStatusRuleDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

// buildMonitorStatusRulePlanWithMonitorID constructs a minimal monitorStatusRuleResourceModel
// with the provided monitor ID.
func buildMonitorStatusRulePlanWithMonitorID(_ context.Context, monitorID string) provider.MonitorStatusRuleResourceModelForTest {
	monitorIDVal := types.StringNull()
	if monitorID != "" {
		monitorIDVal = types.StringValue(monitorID)
	}

	return provider.MonitorStatusRuleResourceModelForTest{
		Predicate: types.StringValue("probe.status == 'Fault'"),
		MonitorID: monitorIDVal,
	}
}

func TestBuildCreateMonitorStatusRuleRelationships_setsMonitorID(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorStatusRulePlanWithMonitorID(ctx, "monitor-1")

	rels, diags := provider.BuildCreateMonitorStatusRuleRelationshipsForTest(ctx, plan)
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

func TestBuildCreateMonitorStatusRuleRelationships_emptyMonitorID(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorStatusRulePlanWithMonitorID(ctx, "")

	rels, diags := provider.BuildCreateMonitorStatusRuleRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	// Even with an empty string, the monitor relationship struct is set (create always requires it)
	if got := rels.Monitor.Data.Id; got != "" {
		t.Errorf("Monitor.Data.Id: want %q, got %q", "", got)
	}
}

func TestBuildUpdateMonitorStatusRuleRelationships_setsMonitorIDWhenPresent(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorStatusRulePlanWithMonitorID(ctx, "monitor-2")

	rels, diags := provider.BuildUpdateMonitorStatusRuleRelationshipsForTest(ctx, plan)
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

func TestBuildUpdateMonitorStatusRuleRelationships_nilMonitorIDSkipsRelationship(t *testing.T) {
	ctx := context.Background()
	plan := buildMonitorStatusRulePlanWithMonitorID(ctx, "")
	// Explicitly set to null to test the nil branch
	plan.MonitorID = types.StringNull()

	rels, diags := provider.BuildUpdateMonitorStatusRuleRelationshipsForTest(ctx, plan)
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

func TestAccMonitorStatusRuleResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify monitor_id round-trip: create with monitor, confirm monitor_id populated,
	// update predicate, confirm drift detection works correctly.
}

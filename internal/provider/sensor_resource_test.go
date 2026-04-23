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
// flattenSensor unit tests
// ---------------------------------------------------------------------------

// sensorFlattenFixture builds a DataInSensorResponse with all fields populated.
func sensorFlattenFixture() api.DataInSensorResponse {
	name := "my-sensor"
	pluginName := "http"
	sensorType := "http-check"
	now := time.Now()

	return api.DataInSensorResponse{
		Id: "sensor-1",
		Attributes: &api.AttributesInSensorResponse{
			Name:           &name,
			PluginName:     &pluginName,
			Type:           &sensorType,
			Parameters:     nil,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInSensorResponse{
			Probe: &api.ToOneProbeInResponse{
				Data: &api.ProbeIdentifierInResponse{Id: "probe-1"},
			},
		},
	}
}

func TestFlattenSensor_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Id = "sensor-99"

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "sensor-99" {
		t.Errorf("ID: want %q, got %q", "sensor-99", got)
	}
}

func TestFlattenSensor_setsNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "my-sensor" {
		t.Errorf("Name: want %q, got %q", "my-sensor", got)
	}
}

func TestFlattenSensor_nullsNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.Name = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.Name.IsNull() {
		t.Errorf("Name: expected null, got %q", model.Name.ValueString())
	}
}

func TestFlattenSensor_setsPluginNameFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.PluginName.ValueString(); got != "http" {
		t.Errorf("PluginName: want %q, got %q", "http", got)
	}
}

func TestFlattenSensor_nullsPluginNameWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.PluginName = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.PluginName.IsNull() {
		t.Errorf("PluginName: expected null, got %q", model.PluginName.ValueString())
	}
}

func TestFlattenSensor_setsTypeFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.Type.ValueString(); got != "http-check" {
		t.Errorf("Type: want %q, got %q", "http-check", got)
	}
}

func TestFlattenSensor_nullsTypeWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.Type = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.Type.IsNull() {
		t.Errorf("Type: expected null, got %q", model.Type.ValueString())
	}
}

func TestFlattenSensor_setsParametersStringWhenPresent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.Parameters = `{"url":"https://example.com"}`

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.Parameters.ValueString(); got != `{"url":"https://example.com"}` {
		t.Errorf("Parameters: want %q, got %q", `{"url":"https://example.com"}`, got)
	}
}

func TestFlattenSensor_nullsParametersWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.Parameters = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.Parameters.IsNull() {
		t.Errorf("Parameters: expected null, got %q", model.Parameters.ValueString())
	}
}

func TestFlattenSensor_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenSensor_nullsTimestampsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Attributes.CreateDateTime = nil
	fixture.Attributes.UpdateDateTime = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.CreateDateTime.IsNull() {
		t.Errorf("CreateDateTime: expected null, got %q", model.CreateDateTime.ValueString())
	}
	if !model.UpdateDateTime.IsNull() {
		t.Errorf("UpdateDateTime: expected null, got %q", model.UpdateDateTime.ValueString())
	}
}

func TestFlattenSensor_setsProbeIDFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.ProbeID.ValueString(); got != "probe-1" {
		t.Errorf("ProbeID: want %q, got %q", "probe-1", got)
	}
}

func TestFlattenSensor_nullsProbeIDWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Relationships.Probe = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.ProbeID.IsNull() {
		t.Errorf("ProbeID: expected null, got %q", model.ProbeID.ValueString())
	}
}

func TestFlattenSensor_nullsProbeIDWhenRelationshipsNil(t *testing.T) {
	ctx := context.Background()
	fixture := sensorFlattenFixture()
	fixture.Relationships = nil

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if !model.ProbeID.IsNull() {
		t.Errorf("ProbeID: expected null when no relationships, got %q", model.ProbeID.ValueString())
	}
}

func TestFlattenSensor_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInSensorResponse{
		Id:         "sensor-1",
		Attributes: nil,
	}

	var model provider.SensorResourceModelForTest
	diags := provider.FlattenSensorForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenSensorForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "sensor-1" {
		t.Errorf("ID: want %q, got %q", "sensor-1", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor tests (unit – no live API)
// ---------------------------------------------------------------------------

func TestSensorResource_constructorReturnsNonNil(t *testing.T) {
	res := provider.NewSensorResource()
	if res == nil {
		t.Fatal("NewSensorResource returned nil")
	}
}

func TestSensorDataSource_constructorReturnsNonNil(t *testing.T) {
	ds := provider.NewSensorDataSource()
	if ds == nil {
		t.Fatal("NewSensorDataSource returned nil")
	}
}

// ---------------------------------------------------------------------------
// Relationship builder unit tests
// ---------------------------------------------------------------------------

func TestBuildCreateSensorRelationships_probeIDSet(t *testing.T) {
	ctx := context.Background()
	plan := provider.SensorResourceModelForTest{
		ProbeID: types.StringValue("probe-42"),
	}

	rels, diags := provider.BuildCreateSensorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := rels.Probe.Data.Id; got != "probe-42" {
		t.Errorf("Probe.Data.Id: want %q, got %q", "probe-42", got)
	}
	if got := string(rels.Probe.Data.Type); got != string(api.ResourceTypeProbes) {
		t.Errorf("Probe.Data.Type: want %q, got %q", api.ResourceTypeProbes, got)
	}
}

func TestBuildUpdateSensorRelationships_probeIDSet(t *testing.T) {
	ctx := context.Background()
	plan := provider.SensorResourceModelForTest{
		ProbeID: types.StringValue("probe-99"),
	}

	rels, diags := provider.BuildUpdateSensorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Probe == nil {
		t.Fatal("Probe: expected non-nil, got nil")
	}
	if got := rels.Probe.Data.Id; got != "probe-99" {
		t.Errorf("Probe.Data.Id: want %q, got %q", "probe-99", got)
	}
}

func TestBuildUpdateSensorRelationships_nilProbeID(t *testing.T) {
	ctx := context.Background()
	plan := provider.SensorResourceModelForTest{
		ProbeID: types.StringNull(),
	}

	rels, diags := provider.BuildUpdateSensorRelationshipsForTest(ctx, plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if rels.Probe != nil {
		t.Error("Probe: expected nil when ProbeID is null, got non-nil")
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

func TestAccSensorResource_basic(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Set TF_ACC=1 to run acceptance tests")
	}
	// TODO: implement full acceptance test (create → plan → update → import → destroy).
	// Verify probe_id round-trip: create with probe, confirm probe_id populated,
	// update name/type, confirm drift detection reflects changes.
}

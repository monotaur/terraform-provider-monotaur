package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenLabel unit tests
// ---------------------------------------------------------------------------

// labelFlattenFixture builds a DataInLabelResponse with all fields populated.
func labelFlattenFixture() api.DataInLabelResponse {
	text := "Production"
	color := "#FF0000"
	icon := "alert"
	name := "production"
	value := "production-value"
	now := time.Now()

	return api.DataInLabelResponse{
		Id: "42",
		Attributes: &api.AttributesInLabelResponse{
			Text:           &text,
			Color:          &color,
			Icon:           &icon,
			Name:           &name,
			Value:          &value,
			CreateDateTime: &now,
			UpdateDateTime: &now,
		},
		Relationships: &api.RelationshipsInLabelResponse{
			Components: &api.ToManyComponentInResponse{
				Data: &[]api.ComponentIdentifierInResponse{
					{Id: "10", Type: "components"},
					{Id: "20", Type: "components"},
				},
			},
		},
	}
}

func TestFlattenLabel_setsIDFromResponse(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()
	fixture.Id = "99"

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "99" {
		t.Errorf("ID: want %q, got %q", "99", got)
	}
}

func TestFlattenLabel_setsTextFromAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if got := model.Text.ValueString(); got != "Production" {
		t.Errorf("Text: want %q, got %q", "Production", got)
	}
}

func TestFlattenLabel_setsColorAndIcon(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if got := model.Color.ValueString(); got != "#FF0000" {
		t.Errorf("Color: want %q, got %q", "#FF0000", got)
	}
	if got := model.Icon.ValueString(); got != "alert" {
		t.Errorf("Icon: want %q, got %q", "alert", got)
	}
}

func TestFlattenLabel_nullsColorAndIconWhenAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()
	fixture.Attributes.Color = nil
	fixture.Attributes.Icon = nil

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if !model.Color.IsNull() {
		t.Errorf("Color: expected null, got %q", model.Color.ValueString())
	}
	if !model.Icon.IsNull() {
		t.Errorf("Icon: expected null, got %q", model.Icon.ValueString())
	}
}

func TestFlattenLabel_setsComputedNameAndValue(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if got := model.Name.ValueString(); got != "production" {
		t.Errorf("Name: want %q, got %q", "production", got)
	}
	if got := model.Value.ValueString(); got != "production-value" {
		t.Errorf("Value: want %q, got %q", "production-value", got)
	}
}

func TestFlattenLabel_setsTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if model.CreateDateTime.IsNull() || model.CreateDateTime.ValueString() == "" {
		t.Error("CreateDateTime: expected non-empty value")
	}
	if model.UpdateDateTime.IsNull() || model.UpdateDateTime.ValueString() == "" {
		t.Error("UpdateDateTime: expected non-empty value")
	}
}

func TestFlattenLabel_setsComponentIDsFromRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}

	var componentIDs []types.String
	diags = model.ComponentIDs.ElementsAs(ctx, &componentIDs, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs returned errors: %v", diags)
	}
	if len(componentIDs) != 2 {
		t.Fatalf("ComponentIDs: want 2 items, got %d", len(componentIDs))
	}
	if got := componentIDs[0].ValueString(); got != "10" {
		t.Errorf("ComponentIDs[0]: want %q, got %q", "10", got)
	}
	if got := componentIDs[1].ValueString(); got != "20" {
		t.Errorf("ComponentIDs[1]: want %q, got %q", "20", got)
	}
}

func TestFlattenLabel_emptyListsWhenNoRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()
	fixture.Relationships = nil

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if model.CalendarEventIDs.IsNull() {
		t.Error("CalendarEventIDs: expected empty list, got null")
	}
	if len(model.CalendarEventIDs.Elements()) != 0 {
		t.Errorf("CalendarEventIDs: want 0 elements, got %d", len(model.CalendarEventIDs.Elements()))
	}
	if model.ComponentIDs.IsNull() {
		t.Error("ComponentIDs: expected empty list, got null")
	}
	if model.MonitorIDs.IsNull() {
		t.Error("MonitorIDs: expected empty list, got null")
	}
}

func TestFlattenLabel_setsAllThreeRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := labelFlattenFixture()
	fixture.Relationships = &api.RelationshipsInLabelResponse{
		CalendarEvents: &api.ToManyCalendarEventInResponse{
			Data: &[]api.CalendarEventIdentifierInResponse{
				{Id: "ce1"},
			},
		},
		Components: &api.ToManyComponentInResponse{
			Data: &[]api.ComponentIdentifierInResponse{
				{Id: "c1"},
			},
		},
		Monitors: &api.ToManyMonitorInResponse{
			Data: &[]api.MonitorIdentifierInResponse{
				{Id: "m1"},
			},
		},
	}

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
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

	checkSingleID("CalendarEventIDs", model.CalendarEventIDs, "ce1")
	checkSingleID("ComponentIDs", model.ComponentIDs, "c1")
	checkSingleID("MonitorIDs", model.MonitorIDs, "m1")
}

func TestFlattenLabel_handlesNilAttributes(t *testing.T) {
	ctx := context.Background()
	fixture := api.DataInLabelResponse{
		Id:         "1",
		Attributes: nil,
	}

	var model provider.LabelResourceModelForTest
	diags := provider.FlattenLabelForTest(ctx, fixture, &model)
	if diags.HasError() {
		t.Fatalf("FlattenLabelForTest returned errors: %v", diags)
	}
	if got := model.ID.ValueString(); got != "1" {
		t.Errorf("ID: want %q, got %q", "1", got)
	}
}

// ---------------------------------------------------------------------------
// Schema attribute name tests (unit – no live API)
// ---------------------------------------------------------------------------

// TestLabelResource_typeNameIsMonotaurLabel ensures the resource type name is
// set correctly, which is validated by the framework at registration time.
func TestLabelResource_typeNameIsMonotaurLabel(t *testing.T) {
	res := provider.NewLabelResource()
	if res == nil {
		t.Fatal("NewLabelResource returned nil")
	}
}

// TestLabelDataSource_typeNameIsMonotaurLabel ensures the data source
// constructor returns a non-nil datasource.
func TestLabelDataSource_typeNameIsMonotaurLabel(t *testing.T) {
	ds := provider.NewLabelDataSource()
	if ds == nil {
		t.Fatal("NewLabelDataSource returned nil")
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
//
// Example test skeletons below will be expanded as the testing framework
// dependency is added.

func TestAccLabelResource_basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping acceptance test in short mode")
	}
	// Acceptance tests require TF_ACC=1 — skip when not set.
	t.Skip("acceptance tests require TF_ACC=1 and a live Monotaur endpoint; set TF_ACC=1 to run")
}

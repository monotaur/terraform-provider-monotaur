package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

func maintenanceScheduleFixture() api.DataInMaintenanceScheduleResponse {
	description := "weekly patching"
	schedule := "0 3 * * 0"
	timeSpan := "01:30:00"
	tz := "Europe/Berlin"
	exclude, apply, squelch := true, true, false
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 13, 30, 0, 0, time.UTC)

	return api.DataInMaintenanceScheduleResponse{
		Id: "5",
		Attributes: &api.AttributesInMaintenanceScheduleResponse{
			Description:            &description,
			Schedule:               &schedule,
			TimeSpan:               &timeSpan,
			TimeZoneId:             &tz,
			ExcludeFromDowntime:    &exclude,
			ApplyToComponentStatus: &apply,
			Squelch:                &squelch,
			CreateDateTime:         &created,
			UpdateDateTime:         &updated,
		},
		Relationships: &api.RelationshipsInMaintenanceScheduleResponse{
			Components: &api.ToManyComponentInResponse{
				Data: &[]api.ComponentIdentifierInResponse{{Id: "c1"}, {Id: "c2"}},
			},
		},
	}
}

func TestFlattenMaintenanceSchedule_allFields(t *testing.T) {
	var m provider.MaintenanceScheduleResourceModelForTest
	if d := provider.FlattenMaintenanceScheduleForTest(context.Background(), maintenanceScheduleFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	got := map[string]string{
		"id":               m.ID.ValueString(),
		"description":      m.Description.ValueString(),
		"schedule":         m.Schedule.ValueString(),
		"time_span":        m.TimeSpan.ValueString(),
		"time_zone_id":     m.TimeZoneID.ValueString(),
		"create_date_time": m.CreateDateTime.ValueString(),
		"update_date_time": m.UpdateDateTime.ValueString(),
	}
	want := map[string]string{
		"id":               "5",
		"description":      "weekly patching",
		"schedule":         "0 3 * * 0",
		"time_span":        "01:30:00",
		"time_zone_id":     "Europe/Berlin",
		"create_date_time": "2026-10-01T12:00:00Z",
		"update_date_time": "2026-10-02T13:30:00Z",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: want %q, got %q", k, v, got[k])
		}
	}
	if !m.ExcludeFromDowntime.ValueBool() || !m.ApplyToComponentStatus.ValueBool() || m.Squelch.ValueBool() {
		t.Errorf("flags: got exclude=%v apply=%v squelch=%v", m.ExcludeFromDowntime, m.ApplyToComponentStatus, m.Squelch)
	}
	if ids := npListIDs(t, m.ComponentIDs); !reflect.DeepEqual(ids, []string{"c1", "c2"}) {
		t.Errorf("component_ids: got %v", ids)
	}
}

func TestFlattenMaintenanceSchedule_keepsEquivalentTimeSpanAndComponentOrder(t *testing.T) {
	m := provider.MaintenanceScheduleResourceModelForTest{
		TimeSpan:     types.StringValue("1:30:0"),
		ComponentIDs: npIDList("c2", "c1"),
	}
	if d := provider.FlattenMaintenanceScheduleForTest(context.Background(), maintenanceScheduleFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if got := m.TimeSpan.ValueString(); got != "1:30:0" {
		t.Errorf("time_span: prior spelling not kept, got %q", got)
	}
	if ids := npListIDs(t, m.ComponentIDs); !reflect.DeepEqual(ids, []string{"c2", "c1"}) {
		t.Errorf("component_ids: prior order not kept, got %v", ids)
	}
}

func TestFlattenMaintenanceSchedule_emptyTimeZoneIsNull(t *testing.T) {
	data := maintenanceScheduleFixture()
	empty := ""
	data.Attributes.TimeZoneId = &empty
	var m provider.MaintenanceScheduleResourceModelForTest
	provider.FlattenMaintenanceScheduleForTest(context.Background(), data, &m)
	if !m.TimeZoneID.IsNull() {
		t.Errorf("time_zone_id: want null, got %v", m.TimeZoneID)
	}

	data.Attributes.TimeZoneId = nil
	provider.FlattenMaintenanceScheduleForTest(context.Background(), data, &m)
	if !m.TimeZoneID.IsNull() {
		t.Errorf("time_zone_id: want null for nil, got %v", m.TimeZoneID)
	}
}

func TestFlattenMaintenanceSchedule_linksOnlyKeepsPrior(t *testing.T) {
	data := maintenanceScheduleFixture()
	data.Relationships = nil

	m := provider.MaintenanceScheduleResourceModelForTest{ComponentIDs: npIDList("c9")}
	provider.FlattenMaintenanceScheduleForTest(context.Background(), data, &m)
	if ids := npListIDs(t, m.ComponentIDs); !reflect.DeepEqual(ids, []string{"c9"}) {
		t.Errorf("component_ids: want prior [c9], got %v", ids)
	}

	m = provider.MaintenanceScheduleResourceModelForTest{ComponentIDs: types.ListNull(types.StringType)}
	provider.FlattenMaintenanceScheduleForTest(context.Background(), data, &m)
	if ids := npListIDs(t, m.ComponentIDs); len(ids) != 0 {
		t.Errorf("component_ids: want empty, got %v", ids)
	}
}

func maintenanceSchedulePlan() provider.MaintenanceScheduleResourceModelForTest {
	return provider.MaintenanceScheduleResourceModelForTest{
		Description:            types.StringValue("weekly patching"),
		Schedule:               types.StringValue("0 3 * * 0"),
		TimeSpan:               types.StringValue("2:0:0"),
		TimeZoneID:             types.StringNull(),
		ExcludeFromDowntime:    types.BoolValue(false),
		ApplyToComponentStatus: types.BoolUnknown(),
		Squelch:                types.BoolValue(true),
		ComponentIDs:           types.ListUnknown(types.StringType),
	}
}

func TestBuildCreateMaintenanceScheduleAttributes(t *testing.T) {
	attrs := provider.BuildCreateMaintenanceScheduleAttributesForTest(maintenanceSchedulePlan())
	if attrs.Description != "weekly patching" || attrs.Schedule != "0 3 * * 0" {
		t.Errorf("description/schedule: got %q / %q", attrs.Description, attrs.Schedule)
	}
	if attrs.TimeSpan == nil || *attrs.TimeSpan != "2:0:0" {
		t.Errorf("time_span: got %v", attrs.TimeSpan)
	}
	if attrs.TimeZoneId != nil {
		t.Errorf("time_zone_id: null should be omitted on create, got %q", *attrs.TimeZoneId)
	}
	if attrs.ExcludeFromDowntime == nil || *attrs.ExcludeFromDowntime {
		t.Errorf("exclude_from_downtime: got %v", attrs.ExcludeFromDowntime)
	}
	if attrs.ApplyToComponentStatus != nil {
		t.Errorf("apply_to_component_status: unknown should be omitted")
	}
	if attrs.Squelch == nil || !*attrs.Squelch {
		t.Errorf("squelch: got %v", attrs.Squelch)
	}
}

func TestMaintenanceScheduleUpdate_clearsTimeZoneWithExplicitNull(t *testing.T) {
	plan := maintenanceSchedulePlan()
	nulls := provider.MaintenanceScheduleNullAttributesForTest(plan)
	if !reflect.DeepEqual(nulls, []string{"timeZoneId"}) {
		t.Fatalf("null attributes: got %v", nulls)
	}

	doc := api.UpdateMaintenanceScheduleRequestDocument{
		Data: api.DataInUpdateMaintenanceScheduleRequest{
			Id:         "5",
			Type:       api.ResourceTypeMaintenanceSchedules,
			Attributes: provider.BuildUpdateMaintenanceScheduleAttributesForTest(plan),
		},
	}
	r, err := client.MarshalWithNullAttributes(doc, nulls)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	var body struct {
		Data struct {
			Attributes map[string]json.RawMessage `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body.Data.Attributes["timeZoneId"]; !ok || string(v) != "null" {
		t.Errorf("timeZoneId: want explicit null, got %s (present=%v)", v, ok)
	}
	if string(body.Data.Attributes["schedule"]) != `"0 3 * * 0"` {
		t.Errorf("schedule: got %s", body.Data.Attributes["schedule"])
	}

	plan.TimeZoneID = types.StringValue("UTC")
	if nulls := provider.MaintenanceScheduleNullAttributesForTest(plan); len(nulls) != 0 {
		t.Errorf("null attributes with time zone set: got %v", nulls)
	}
}

func TestBuildMaintenanceScheduleComponents(t *testing.T) {
	ctx := context.Background()
	plan := maintenanceSchedulePlan()

	rel, d := provider.BuildMaintenanceScheduleComponentsForTest(ctx, plan)
	if d.HasError() || rel != nil {
		t.Errorf("unknown list: want nil relationship, got %v (%v)", rel, d)
	}

	plan.ComponentIDs = npIDList()
	rel, _ = provider.BuildMaintenanceScheduleComponentsForTest(ctx, plan)
	if rel == nil || rel.Data == nil || len(rel.Data) != 0 {
		t.Fatalf("empty list: want `data: []`, got %v", rel)
	}
	b, _ := json.Marshal(rel)
	if string(b) != `{"data":[]}` {
		t.Errorf("empty list JSON: got %s", b)
	}

	plan.ComponentIDs = npIDList("c1", "c2")
	rel, _ = provider.BuildMaintenanceScheduleComponentsForTest(ctx, plan)
	if rel == nil || len(rel.Data) != 2 || rel.Data[1].Id != "c2" || rel.Data[0].Type != api.ResourceTypeComponents {
		t.Errorf("two components: got %+v", rel)
	}
}

func TestMaintenanceScheduleReadParams_includesComponents(t *testing.T) {
	p := provider.MaintenanceScheduleReadParamsForTest()
	if p.Query == nil {
		t.Fatal("query: nil")
	}
	q := *p.Query
	if q["include"] == nil || *q["include"] != "components" {
		t.Errorf("include: got %v", q["include"])
	}
	if q["page[size]"] == nil || *q["page[size]"] != "components:500" {
		t.Errorf("page[size]: got %v", q["page[size]"])
	}
}

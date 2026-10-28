package provider_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

func alarmNoteFixture() api.DataInAlarmNoteResponse {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)
	kind := api.Mitigating
	body := "Rolled back **v2.3**."
	public := true
	author := "service-account:4"
	return api.DataInAlarmNoteResponse{
		Id: "21",
		Attributes: &api.AttributesInAlarmNoteResponse{
			Kind:           &kind,
			Body:           &body,
			Public:         &public,
			Author:         &author,
			CreateDateTime: &created,
			UpdateDateTime: &updated,
		},
		Relationships: &api.RelationshipsInAlarmNoteResponse{
			Alarm: &api.ToOneAlarmInResponse{Data: &api.AlarmIdentifierInResponse{Id: "9"}},
		},
	}
}

func TestFlattenAlarmNote_allFields(t *testing.T) {
	var m provider.AlarmNoteResourceModelForTest
	if d := provider.FlattenAlarmNoteForTest(context.Background(), alarmNoteFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.ID.ValueString() != "21" || m.AlarmID.ValueString() != "9" {
		t.Errorf("ids: got %q, alarm %q", m.ID.ValueString(), m.AlarmID.ValueString())
	}
	if m.Kind.ValueString() != "Mitigating" || m.Body.ValueString() != "Rolled back **v2.3**." || !m.Public.ValueBool() {
		t.Errorf("attributes: got %+v", m)
	}
	if m.Author.ValueString() != "service-account:4" {
		t.Errorf("Author: got %q", m.Author.ValueString())
	}
	if m.CreateDateTime.ValueString() != "2026-10-01T12:00:00Z" || m.UpdateDateTime.ValueString() != "2026-10-01T12:05:00Z" {
		t.Errorf("timestamps: got %q, %q", m.CreateDateTime.ValueString(), m.UpdateDateTime.ValueString())
	}
}

func TestFlattenAlarmNote_linksOnlyKeepsPriorAlarmID(t *testing.T) {
	data := alarmNoteFixture()
	data.Relationships = &api.RelationshipsInAlarmNoteResponse{Alarm: &api.ToOneAlarmInResponse{}}
	m := provider.AlarmNoteResourceModelForTest{AlarmID: types.StringValue("9")}
	provider.FlattenAlarmNoteForTest(context.Background(), data, &m)
	if m.AlarmID.ValueString() != "9" {
		t.Errorf("AlarmID: got %q, want prior value kept", m.AlarmID.ValueString())
	}

	m = provider.AlarmNoteResourceModelForTest{AlarmID: types.StringUnknown()}
	data.Relationships = nil
	provider.FlattenAlarmNoteForTest(context.Background(), data, &m)
	if !m.AlarmID.IsNull() {
		t.Errorf("AlarmID: want null instead of unknown, got %v", m.AlarmID)
	}
}

func TestFlattenAlarmNote_nilAttributesAreNull(t *testing.T) {
	var m provider.AlarmNoteResourceModelForTest
	provider.FlattenAlarmNoteForTest(context.Background(), api.DataInAlarmNoteResponse{Id: "1"}, &m)
	if !m.Kind.IsNull() || !m.Body.IsNull() || !m.Public.IsNull() || !m.Author.IsNull() || !m.CreateDateTime.IsNull() || !m.UpdateDateTime.IsNull() {
		t.Errorf("want null attributes, got %+v", m)
	}
}

func TestBuildCreateAlarmNoteBody(t *testing.T) {
	plan := provider.AlarmNoteResourceModelForTest{
		AlarmID: types.StringValue("9"),
		Kind:    types.StringValue("Investigating"),
		Body:    types.StringValue("Looking"),
		Public:  types.BoolUnknown(),
	}
	doc := provider.BuildCreateAlarmNoteBodyForTest(plan)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Data struct {
			Attributes    map[string]any `json:"attributes"`
			Relationships struct {
				Alarm struct {
					Data struct{ ID, Type string } `json:"data"`
				} `json:"alarm"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Data.Attributes["kind"] != "Investigating" || got.Data.Attributes["body"] != "Looking" {
		t.Errorf("attributes: got %v", got.Data.Attributes)
	}
	if _, ok := got.Data.Attributes["public"]; ok {
		t.Errorf("public: want omitted when unknown, got %v", got.Data.Attributes["public"])
	}
	if got.Data.Relationships.Alarm.Data.ID != "9" || got.Data.Relationships.Alarm.Data.Type != "alarms" {
		t.Errorf("alarm: got %+v", got.Data.Relationships.Alarm.Data)
	}

	plan.Public = types.BoolValue(true)
	doc = provider.BuildCreateAlarmNoteBodyForTest(plan)
	if doc.Data.Attributes.Public == nil || !*doc.Data.Attributes.Public {
		t.Errorf("public: want true")
	}
}

func TestBuildUpdateAlarmNoteBody_neverSendsAlarm(t *testing.T) {
	plan := provider.AlarmNoteResourceModelForTest{
		AlarmID: types.StringValue("9"),
		Kind:    types.StringValue("Resolved"),
		Body:    types.StringValue("Fixed"),
		Public:  types.BoolValue(false),
	}
	doc := provider.BuildUpdateAlarmNoteBodyForTest("21", plan)
	if doc.Data.Id != "21" || doc.Data.Relationships != nil {
		t.Errorf("data: got id %q, relationships %v", doc.Data.Id, doc.Data.Relationships)
	}
	a := doc.Data.Attributes
	if a.Kind == nil || *a.Kind != api.Resolved || a.Body == nil || *a.Body != "Fixed" || a.Public == nil || *a.Public {
		t.Errorf("attributes: got %+v", a)
	}
}

func TestAlarmNoteReadParams_includesAlarm(t *testing.T) {
	p := provider.AlarmNoteReadParamsForTest()
	if p.Query == nil || (*p.Query)["include"] == nil || *(*p.Query)["include"] != "alarm" {
		t.Errorf("want include=alarm, got %+v", p.Query)
	}
}

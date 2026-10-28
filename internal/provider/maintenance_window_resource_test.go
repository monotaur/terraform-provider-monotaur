package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

func maintenanceWindowFixture() api.DataInMaintenanceWindowResponse {
	description := "db upgrade"
	start := time.Date(2030, 1, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2030, 1, 1, 9, 30, 0, 500_000_000, time.UTC)
	reason := api.EmergencyMaintenance
	exclude, apply, squelch := true, false, true
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 13, 30, 0, 0, time.UTC)

	return api.DataInMaintenanceWindowResponse{
		Id: "12",
		Attributes: &api.AttributesInMaintenanceWindowResponse{
			Description:            &description,
			StartDateTime:          &start,
			EndDateTime:            &end,
			Reason:                 &reason,
			ExcludeFromDowntime:    &exclude,
			ApplyToComponentStatus: &apply,
			Squelch:                &squelch,
			CreateDateTime:         &created,
			UpdateDateTime:         &updated,
		},
		Relationships: &api.RelationshipsInMaintenanceWindowResponse{
			MaintenanceSchedule: &api.NullableToOneMaintenanceScheduleInResponse{
				Data: &api.MaintenanceScheduleIdentifierInResponse{Id: "3"},
			},
		},
	}
}

func TestFlattenMaintenanceWindow_allFields(t *testing.T) {
	var m provider.MaintenanceWindowResourceModelForTest
	if d := provider.FlattenMaintenanceWindowForTest(context.Background(), maintenanceWindowFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	want := map[string]string{
		"id":                      m.ID.ValueString(),
		"description":             m.Description.ValueString(),
		"start_date_time":         m.StartDateTime.ValueString(),
		"end_date_time":           m.EndDateTime.ValueString(),
		"reason":                  m.Reason.ValueString(),
		"maintenance_schedule_id": m.MaintenanceScheduleID.ValueString(),
		"create_date_time":        m.CreateDateTime.ValueString(),
		"update_date_time":        m.UpdateDateTime.ValueString(),
	}
	expected := map[string]string{
		"id":                      "12",
		"description":             "db upgrade",
		"start_date_time":         "2030-01-01T08:00:00Z",
		"end_date_time":           "2030-01-01T09:30:00.5Z",
		"reason":                  "EmergencyMaintenance",
		"maintenance_schedule_id": "3",
		"create_date_time":        "2026-10-01T12:00:00Z",
		"update_date_time":        "2026-10-02T13:30:00Z",
	}
	for k, v := range expected {
		if want[k] != v {
			t.Errorf("%s: want %q, got %q", k, v, want[k])
		}
	}
	if !m.ExcludeFromDowntime.ValueBool() || m.ApplyToComponentStatus.ValueBool() || !m.Squelch.ValueBool() {
		t.Errorf("flags: got exclude=%v apply=%v squelch=%v", m.ExcludeFromDowntime, m.ApplyToComponentStatus, m.Squelch)
	}
}

func TestFlattenMaintenanceWindow_keepsPriorTimestampSpelling(t *testing.T) {
	m := provider.MaintenanceWindowResourceModelForTest{
		StartDateTime: types.StringValue("2030-01-01T10:00:00+02:00"),
		EndDateTime:   types.StringValue("2030-01-01T10:30:00.500+01:00"),
	}
	if d := provider.FlattenMaintenanceWindowForTest(context.Background(), maintenanceWindowFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if got := m.StartDateTime.ValueString(); got != "2030-01-01T10:00:00+02:00" {
		t.Errorf("start: prior spelling not kept, got %q", got)
	}
	if got := m.EndDateTime.ValueString(); got != "2030-01-01T10:30:00.500+01:00" {
		t.Errorf("end: prior spelling not kept, got %q", got)
	}
}

func TestFlattenMaintenanceTimestamp(t *testing.T) {
	instant := time.Date(2030, 1, 1, 8, 0, 0, 0, time.FixedZone("", 0))
	cases := []struct {
		name  string
		v     *time.Time
		prior types.String
		want  types.String
	}{
		{"nil is null", nil, types.StringValue("2030-01-01T08:00:00Z"), types.StringNull()},
		{"null prior formats UTC", &instant, types.StringNull(), types.StringValue("2030-01-01T08:00:00Z")},
		{"equal instant keeps prior", &instant, types.StringValue("2030-01-01T09:00:00+01:00"), types.StringValue("2030-01-01T09:00:00+01:00")},
		{"different instant uses API", &instant, types.StringValue("2030-01-01T09:00:00Z"), types.StringValue("2030-01-01T08:00:00Z")},
		{"unparseable prior uses API", &instant, types.StringValue("tomorrow"), types.StringValue("2030-01-01T08:00:00Z")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := provider.FlattenMaintenanceTimestampForTest(tc.v, tc.prior); !got.Equal(tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestFlattenMaintenanceWindow_nilAttributesAndRelationships(t *testing.T) {
	m := provider.MaintenanceWindowResourceModelForTest{MaintenanceScheduleID: types.StringValue("3")}
	data := api.DataInMaintenanceWindowResponse{Id: "1"}
	if d := provider.FlattenMaintenanceWindowForTest(context.Background(), data, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.ID.ValueString() != "1" {
		t.Errorf("id: got %q", m.ID.ValueString())
	}
	if !m.MaintenanceScheduleID.IsNull() {
		t.Errorf("maintenance_schedule_id: want null when the relationship is absent, got %v", m.MaintenanceScheduleID)
	}
}

func maintenanceWindowPlan() provider.MaintenanceWindowResourceModelForTest {
	return provider.MaintenanceWindowResourceModelForTest{
		Description:            types.StringValue("db upgrade"),
		StartDateTime:          types.StringValue("2030-01-01T10:00:00+02:00"),
		EndDateTime:            types.StringValue("2030-01-01T11:00:00+02:00"),
		Reason:                 types.StringUnknown(),
		ExcludeFromDowntime:    types.BoolValue(true),
		ApplyToComponentStatus: types.BoolUnknown(),
		Squelch:                types.BoolNull(),
	}
}

func TestBuildCreateMaintenanceWindowAttributes(t *testing.T) {
	attrs, d := provider.BuildCreateMaintenanceWindowAttributesForTest(maintenanceWindowPlan())
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if attrs.Description != "db upgrade" {
		t.Errorf("description: got %q", attrs.Description)
	}
	if attrs.StartDateTime == nil || !attrs.StartDateTime.Equal(time.Date(2030, 1, 1, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("start: got %v", attrs.StartDateTime)
	}
	if attrs.EndDateTime == nil || !attrs.EndDateTime.Equal(time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("end: got %v", attrs.EndDateTime)
	}
	if attrs.Reason != nil {
		t.Errorf("reason: unknown should be omitted, got %v", *attrs.Reason)
	}
	if attrs.ExcludeFromDowntime == nil || !*attrs.ExcludeFromDowntime {
		t.Errorf("exclude_from_downtime: got %v", attrs.ExcludeFromDowntime)
	}
	if attrs.ApplyToComponentStatus != nil || attrs.Squelch != nil {
		t.Errorf("unknown/null flags should be omitted, got apply=%v squelch=%v", attrs.ApplyToComponentStatus, attrs.Squelch)
	}
}

func TestBuildUpdateMaintenanceWindowAttributes_sendsStartAndEndTogether(t *testing.T) {
	plan := maintenanceWindowPlan()
	plan.Reason = types.StringValue("ExternalIssue")
	attrs, d := provider.BuildUpdateMaintenanceWindowAttributesForTest(plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if attrs.StartDateTime == nil || attrs.EndDateTime == nil {
		t.Fatalf("start and end must both be sent, got start=%v end=%v", attrs.StartDateTime, attrs.EndDateTime)
	}
	if attrs.Reason == nil || *attrs.Reason != api.ExternalIssue {
		t.Errorf("reason: got %v", attrs.Reason)
	}
	if attrs.Description == nil || *attrs.Description != "db upgrade" {
		t.Errorf("description: got %v", attrs.Description)
	}
}

func TestBuildMaintenanceWindowAttributes_invalidTimestamp(t *testing.T) {
	plan := maintenanceWindowPlan()
	plan.StartDateTime = types.StringValue("2030-01-01 10:00")
	if _, d := provider.BuildCreateMaintenanceWindowAttributesForTest(plan); !d.HasError() {
		t.Error("expected an error for a non-RFC 3339 timestamp")
	}
}

func TestMaintenanceWindowReadParams_includesSchedule(t *testing.T) {
	p := provider.MaintenanceWindowReadParamsForTest()
	if p.Query == nil || (*p.Query)["include"] == nil || *(*p.Query)["include"] != "maintenanceSchedule" {
		t.Fatalf("include: got %v", p.Query)
	}
}

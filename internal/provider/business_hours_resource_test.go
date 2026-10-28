package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

func bhStrPtr(s string) *string { return &s }

func bhDays(v string) *api.DaysOfWeek {
	d := api.DaysOfWeek(v)
	return &d
}

func bhSetValues(t *testing.T, s types.Set) []string {
	t.Helper()
	var out []string
	if d := s.ElementsAs(context.Background(), &out, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	return out
}

func bhAssertDays(t *testing.T, s types.Set, want ...string) {
	t.Helper()
	got := bhSetValues(t, s)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("days_of_week: got %v, want %v", got, want)
	}
}

func businessHoursFixture() api.DataInBusinessHourResponse {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)
	return api.DataInBusinessHourResponse{
		Id: "3",
		Attributes: &api.AttributesInBusinessHourResponse{
			Name:           bhStrPtr("Office"),
			DaysOfWeek:     bhDays("Monday, Wednesday"),
			StartTime:      bhStrPtr("09:00:00"),
			EndTime:        bhStrPtr("17:30:00"),
			TimeZoneId:     bhStrPtr("Europe/Berlin"),
			CreateDateTime: &created,
			UpdateDateTime: &updated,
		},
	}
}

func TestFlattenBusinessHours_allFields(t *testing.T) {
	var m provider.BusinessHoursResourceModelForTest
	if d := provider.FlattenBusinessHoursForTest(context.Background(), businessHoursFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.ID.ValueString() != "3" {
		t.Errorf("ID: got %q", m.ID.ValueString())
	}
	if m.Name.ValueString() != "Office" {
		t.Errorf("Name: got %q", m.Name.ValueString())
	}
	bhAssertDays(t, m.DaysOfWeek, "Monday", "Wednesday")
	if m.StartTime.ValueString() != "09:00:00" || m.EndTime.ValueString() != "17:30:00" {
		t.Errorf("times: got %q-%q", m.StartTime.ValueString(), m.EndTime.ValueString())
	}
	if m.TimeZoneID.ValueString() != "Europe/Berlin" {
		t.Errorf("TimeZoneID: got %q", m.TimeZoneID.ValueString())
	}
	if m.CreateDateTime.ValueString() != "2026-10-01T12:00:00Z" || m.UpdateDateTime.ValueString() != "2026-10-01T12:05:00Z" {
		t.Errorf("timestamps: got %q, %q", m.CreateDateTime.ValueString(), m.UpdateDateTime.ValueString())
	}
}

func TestFlattenBusinessHours_compositeDaysAreExpanded(t *testing.T) {
	cases := map[string][]string{
		"Weekdays":           {"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"},
		"Weekends":           {"Sunday", "Saturday"},
		"Everyday":           {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		"Weekdays, Saturday": {"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		"Sunday":             {"Sunday"},
		"Friday, Sunday":     {"Sunday", "Friday"},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			data := businessHoursFixture()
			data.Attributes.DaysOfWeek = bhDays(in)
			var m provider.BusinessHoursResourceModelForTest
			if d := provider.FlattenBusinessHoursForTest(context.Background(), data, &m); d.HasError() {
				t.Fatalf("flatten: %v", d)
			}
			bhAssertDays(t, m.DaysOfWeek, want...)
		})
	}
}

func TestFlattenBusinessHours_timeSpanKeepsPriorSpelling(t *testing.T) {
	m := provider.BusinessHoursResourceModelForTest{
		StartTime: types.StringValue("9:0:0"),
		EndTime:   types.StringValue("17:00:00"),
	}
	data := businessHoursFixture()
	data.Attributes.EndTime = bhStrPtr("1.00:00:00")
	if d := provider.FlattenBusinessHoursForTest(context.Background(), data, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.StartTime.ValueString() != "9:0:0" {
		t.Errorf("StartTime: got %q, want the prior spelling", m.StartTime.ValueString())
	}
	if m.EndTime.ValueString() != "1.00:00:00" {
		t.Errorf("EndTime: got %q, want the API value when the duration differs", m.EndTime.ValueString())
	}
}

func TestFlattenBusinessHours_nilAndEmptyAreNull(t *testing.T) {
	data := api.DataInBusinessHourResponse{Id: "4", Attributes: &api.AttributesInBusinessHourResponse{TimeZoneId: bhStrPtr("")}}
	var m provider.BusinessHoursResourceModelForTest
	if d := provider.FlattenBusinessHoursForTest(context.Background(), data, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if !m.TimeZoneID.IsNull() {
		t.Errorf("TimeZoneID: want null for empty string, got %q", m.TimeZoneID.ValueString())
	}
	if !m.DaysOfWeek.IsNull() || !m.StartTime.IsNull() || !m.Name.IsNull() || !m.CreateDateTime.IsNull() {
		t.Errorf("want null attributes, got %+v", m)
	}

	m = provider.BusinessHoursResourceModelForTest{}
	if d := provider.FlattenBusinessHoursForTest(context.Background(), api.DataInBusinessHourResponse{Id: "5"}, &m); d.HasError() {
		t.Fatalf("flatten nil attributes: %v", d)
	}
	if m.ID.ValueString() != "5" || !m.TimeZoneID.IsNull() {
		t.Errorf("nil attributes: got %+v", m)
	}
}

func bhPlan(t *testing.T, tz types.String, days ...string) provider.BusinessHoursResourceModelForTest {
	t.Helper()
	set, d := types.SetValueFrom(context.Background(), types.StringType, days)
	if d.HasError() {
		t.Fatalf("SetValueFrom: %v", d)
	}
	return provider.BusinessHoursResourceModelForTest{
		Name:       types.StringValue("Office"),
		DaysOfWeek: set,
		StartTime:  types.StringValue("09:00:00"),
		EndTime:    types.StringValue("17:00:00"),
		TimeZoneID: tz,
	}
}

func TestBuildCreateBusinessHoursBody_daysInFlagOrder(t *testing.T) {
	doc, d := provider.BuildCreateBusinessHoursBodyForTest(context.Background(), bhPlan(t, types.StringNull(), "Saturday", "Monday", "Sunday"))
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	a := doc.Data.Attributes
	if a.DaysOfWeek == nil || string(*a.DaysOfWeek) != "Sunday, Monday, Saturday" {
		t.Errorf("DaysOfWeek: got %v", a.DaysOfWeek)
	}
	if a.Name != "Office" || *a.StartTime != "09:00:00" || *a.EndTime != "17:00:00" {
		t.Errorf("attributes: got %+v", a)
	}
	if a.TimeZoneId != nil {
		t.Errorf("TimeZoneId: want omitted on create, got %q", *a.TimeZoneId)
	}
	if doc.Data.Relationships != nil {
		t.Errorf("Relationships: components must not be sent")
	}
}

func TestBuildUpdateBusinessHoursBody_clearsTimeZone(t *testing.T) {
	doc, nulls, d := provider.BuildUpdateBusinessHoursBodyForTest(context.Background(), "3", bhPlan(t, types.StringNull(), "Monday"))
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	r, err := client.MarshalWithNullAttributes(doc, nulls)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw, _ := io.ReadAll(r)
	var got struct {
		Data struct {
			ID            string                     `json:"id"`
			Attributes    map[string]json.RawMessage `json:"attributes"`
			Relationships json.RawMessage            `json:"relationships"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Data.ID != "3" {
		t.Errorf("id: got %q", got.Data.ID)
	}
	if string(got.Data.Attributes["timeZoneId"]) != "null" {
		t.Errorf("timeZoneId: want explicit null, got %s", got.Data.Attributes["timeZoneId"])
	}
	if string(got.Data.Attributes["daysOfWeek"]) != `"Monday"` {
		t.Errorf("daysOfWeek: got %s", got.Data.Attributes["daysOfWeek"])
	}
	if got.Data.Relationships != nil {
		t.Errorf("relationships: want omitted, got %s", got.Data.Relationships)
	}

	_, nulls, _ = provider.BuildUpdateBusinessHoursBodyForTest(context.Background(), "3", bhPlan(t, types.StringValue("UTC"), "Monday"))
	if len(nulls) != 0 {
		t.Errorf("nulls: want none when time_zone_id is set, got %v", nulls)
	}
}

func TestBusinessHoursTimeSpanValidator(t *testing.T) {
	cases := []struct {
		value string
		end   bool
		ok    bool
	}{
		{"09:00:00", false, true},
		{"00:00:00", false, true},
		{"23:59:59", false, true},
		{"9:0:0", false, true},
		{"1.00:00:00", false, false},
		{"24:00:00", false, false},
		{"-01:00:00", false, false},
		{"09:00", false, false},
		{"17:00:00", true, true},
		{"1.00:00:00", true, true},
		{"00:00:00", true, false},
		{"24:00:00", true, false},
		{"1.00:00:01", true, false},
		{"12:60:00", true, false},
		{"noon", true, false},
	}
	for _, tc := range cases {
		v := provider.BusinessHoursTimeSpanValidatorForTest(tc.end)
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(tc.value)}, resp)
		if got := !resp.Diagnostics.HasError(); got != tc.ok {
			t.Errorf("%q (end=%t): valid=%t, want %t", tc.value, tc.end, got, tc.ok)
		}
	}
}

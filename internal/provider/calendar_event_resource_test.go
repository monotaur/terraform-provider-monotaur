package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func calendarEventFixture() api.DataInCalendarEventResponse {
	title := "Release 4.2"
	description := "Rolled out to all regions"
	ref := "CHG-1234"
	start := time.Date(2031, 1, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2031, 1, 1, 9, 30, 0, 0, time.UTC)
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 13, 30, 0, 0, time.UTC)

	return api.DataInCalendarEventResponse{
		Id: "5",
		Attributes: &api.AttributesInCalendarEventResponse{
			Title:             &title,
			Description:       &description,
			StartDateTime:     &start,
			EndDateTime:       &end,
			ExternalReference: &ref,
			CreateDateTime:    &created,
			UpdateDateTime:    &updated,
		},
		Relationships: &api.RelationshipsInCalendarEventResponse{
			Labels: &api.ToManyLabelInResponse{
				Data: &[]api.LabelIdentifierInResponse{{Id: "l1"}, {Id: "l2"}},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// flattenCalendarEvent
// ---------------------------------------------------------------------------

func TestFlattenCalendarEvent_allFields(t *testing.T) {
	var m provider.CalendarEventResourceModelForTest
	if d := provider.FlattenCalendarEventForTest(context.Background(), calendarEventFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}

	want := map[string]types.String{
		"id":                 types.StringValue("5"),
		"title":              types.StringValue("Release 4.2"),
		"description":        types.StringValue("Rolled out to all regions"),
		"start_date_time":    types.StringValue("2031-01-01T08:00:00Z"),
		"end_date_time":      types.StringValue("2031-01-01T09:30:00Z"),
		"external_reference": types.StringValue("CHG-1234"),
		"create_date_time":   types.StringValue("2026-10-01T12:00:00Z"),
		"update_date_time":   types.StringValue("2026-10-02T13:30:00Z"),
	}
	got := map[string]types.String{
		"id":                 m.ID,
		"title":              m.Title,
		"description":        m.Description,
		"start_date_time":    m.StartDateTime,
		"end_date_time":      m.EndDateTime,
		"external_reference": m.ExternalReference,
		"create_date_time":   m.CreateDateTime,
		"update_date_time":   m.UpdateDateTime,
	}
	for k, w := range want {
		if !got[k].Equal(w) {
			t.Errorf("%s: want %s, got %s", k, w, got[k])
		}
	}
	npAssertIDs(t, "label_ids", m.LabelIDs, "l1", "l2")
}

func TestFlattenCalendarEvent_nilOptionalAttributesAreNull(t *testing.T) {
	data := calendarEventFixture()
	data.Attributes.Description = nil
	data.Attributes.ExternalReference = nil

	m := provider.CalendarEventResourceModelForTest{
		Description:       types.StringValue("old"),
		ExternalReference: types.StringValue("old"),
	}
	if d := provider.FlattenCalendarEventForTest(context.Background(), data, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if !m.Description.IsNull() {
		t.Errorf("description: want null, got %s", m.Description)
	}
	if !m.ExternalReference.IsNull() {
		t.Errorf("external_reference: want null, got %s", m.ExternalReference)
	}
}

func TestFlattenCalendarEvent_nilAttributesAndRelationships(t *testing.T) {
	m := provider.CalendarEventResourceModelForTest{LabelIDs: types.ListUnknown(types.StringType)}
	if d := provider.FlattenCalendarEventForTest(context.Background(), api.DataInCalendarEventResponse{Id: "9"}, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.ID.ValueString() != "9" {
		t.Errorf("id: got %s", m.ID)
	}
	for name, v := range map[string]types.String{
		"title": m.Title, "description": m.Description, "start_date_time": m.StartDateTime,
		"end_date_time": m.EndDateTime, "external_reference": m.ExternalReference,
		"create_date_time": m.CreateDateTime, "update_date_time": m.UpdateDateTime,
	} {
		if !v.IsNull() {
			t.Errorf("%s: want null, got %s", name, v)
		}
	}
	npAssertIDs(t, "label_ids", m.LabelIDs)
}

func TestFlattenCalendarEvent_keepsPriorTimestampSpelling(t *testing.T) {
	m := provider.CalendarEventResourceModelForTest{
		StartDateTime: types.StringValue("2031-01-01T10:00:00+02:00"),
		EndDateTime:   types.StringValue("2031-01-01T12:00:00Z"), // a different instant: replaced
	}
	if d := provider.FlattenCalendarEventForTest(context.Background(), calendarEventFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if got := m.StartDateTime.ValueString(); got != "2031-01-01T10:00:00+02:00" {
		t.Errorf("start_date_time: want configured spelling kept, got %s", got)
	}
	if got := m.EndDateTime.ValueString(); got != "2031-01-01T09:30:00Z" {
		t.Errorf("end_date_time: want API value, got %s", got)
	}
}

func TestFlattenCalendarEvent_labels(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		prior types.List
		data  *[]api.LabelIdentifierInResponse // nil: links only
		want  []string
	}{
		{"links only keeps prior", npIDList("a"), nil, []string{"a"}},
		{"links only with null prior is empty", types.ListNull(types.StringType), nil, nil},
		{"same IDs keep prior order", npIDList("l2", "l1"), &[]api.LabelIdentifierInResponse{{Id: "l1"}, {Id: "l2"}}, []string{"l2", "l1"}},
		{"drift replaces prior", npIDList("l1"), &[]api.LabelIdentifierInResponse{{Id: "l3"}}, []string{"l3"}},
		{"empty data clears prior", npIDList("l1"), &[]api.LabelIdentifierInResponse{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := calendarEventFixture()
			data.Relationships.Labels.Data = tc.data
			m := provider.CalendarEventResourceModelForTest{LabelIDs: tc.prior}
			if d := provider.FlattenCalendarEventForTest(ctx, data, &m); d.HasError() {
				t.Fatalf("flatten: %v", d)
			}
			npAssertIDs(t, "label_ids", m.LabelIDs, tc.want...)
		})
	}
}

// ---------------------------------------------------------------------------
// Request builders
// ---------------------------------------------------------------------------

func calendarEventPlan() provider.CalendarEventResourceModelForTest {
	return provider.CalendarEventResourceModelForTest{
		Title:             types.StringValue("Release"),
		Description:       types.StringValue("desc"),
		StartDateTime:     types.StringValue("2031-01-01T10:00:00+02:00"),
		EndDateTime:       types.StringValue("2031-01-01T11:00:00+02:00"),
		ExternalReference: types.StringValue("CHG-1"),
		LabelIDs:          npIDList("l1"),
	}
}

func TestBuildCreateCalendarEventBody(t *testing.T) {
	doc, d := provider.BuildCreateCalendarEventBodyForTest(context.Background(), calendarEventPlan())
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	a := doc.Data.Attributes
	if a.Title != "Release" || *a.Description != "desc" || *a.ExternalReference != "CHG-1" {
		t.Errorf("attributes: %+v", a)
	}
	if !a.StartDateTime.Equal(time.Date(2031, 1, 1, 8, 0, 0, 0, time.UTC)) || !a.EndDateTime.Equal(time.Date(2031, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("timestamps: %v %v", a.StartDateTime, a.EndDateTime)
	}
	labels := doc.Data.Relationships.Labels
	if labels == nil || len(labels.Data) != 1 || labels.Data[0].Id != "l1" || labels.Data[0].Type != api.ResourceTypeLabels {
		t.Errorf("labels: %+v", labels)
	}
}

func TestBuildCreateCalendarEventBody_omitsUnsetOptionals(t *testing.T) {
	plan := calendarEventPlan()
	plan.Description = types.StringNull()
	plan.ExternalReference = types.StringNull()
	plan.LabelIDs = types.ListUnknown(types.StringType)
	doc, d := provider.BuildCreateCalendarEventBodyForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if doc.Data.Attributes.Description != nil || doc.Data.Attributes.ExternalReference != nil {
		t.Errorf("want nil optionals, got %+v", doc.Data.Attributes)
	}
	if doc.Data.Relationships.Labels != nil {
		t.Errorf("want labels omitted, got %+v", doc.Data.Relationships.Labels)
	}
}

func TestBuildCalendarEventBody_invalidTimestamp(t *testing.T) {
	plan := calendarEventPlan()
	plan.StartDateTime = types.StringValue("tomorrow")
	if _, d := provider.BuildCreateCalendarEventBodyForTest(context.Background(), plan); !d.HasError() {
		t.Error("create: want an error for an invalid timestamp")
	}
	if _, _, d := provider.BuildUpdateCalendarEventBodyForTest(context.Background(), "5", plan); !d.HasError() {
		t.Error("update: want an error for an invalid timestamp")
	}
}

// TestCalendarEventUpdateBody_sendsExplicitNulls checks the full PATCH body
// when description and external_reference are removed from configuration and
// label_ids is set to [].
func TestCalendarEventUpdateBody_sendsExplicitNulls(t *testing.T) {
	plan := calendarEventPlan()
	plan.Description = types.StringNull()
	plan.ExternalReference = types.StringNull()
	plan.LabelIDs = npIDList()

	doc, nulls, d := provider.BuildUpdateCalendarEventBodyForTest(context.Background(), "5", plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	r, err := client.MarshalWithNullAttributes(doc, nulls)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)

	var decoded struct {
		Data struct {
			ID            string                     `json:"id"`
			Attributes    map[string]json.RawMessage `json:"attributes"`
			Relationships struct {
				Labels *struct {
					Data []json.RawMessage `json:"data"`
				} `json:"labels"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	if decoded.Data.ID != "5" {
		t.Errorf("id: got %q", decoded.Data.ID)
	}
	for _, k := range []string{"description", "externalReference"} {
		if v, ok := decoded.Data.Attributes[k]; !ok || string(v) != "null" {
			t.Errorf("attribute %s: want explicit null, got %q (present=%v) in %s", k, v, ok, b)
		}
	}
	for _, k := range []string{"title", "startDateTime", "endDateTime"} {
		if _, ok := decoded.Data.Attributes[k]; !ok {
			t.Errorf("attribute %s: want present in %s", k, b)
		}
	}
	if decoded.Data.Relationships.Labels == nil || decoded.Data.Relationships.Labels.Data == nil || len(decoded.Data.Relationships.Labels.Data) != 0 {
		t.Errorf("labels: want `data: []`, got %s", b)
	}
}

func TestBuildUpdateCalendarEventBody_setOptionalsAreNotNulled(t *testing.T) {
	_, nulls, d := provider.BuildUpdateCalendarEventBodyForTest(context.Background(), "5", calendarEventPlan())
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if len(nulls) != 0 {
		t.Errorf("want no nulls, got %v", nulls)
	}
}

func TestCalendarEventConfiguredLabels(t *testing.T) {
	plan := calendarEventPlan()

	got := provider.CalendarEventConfiguredLabelsForTest(plan, provider.CalendarEventResourceModelForTest{LabelIDs: types.ListNull(types.StringType)})
	if !got.LabelIDs.IsNull() {
		t.Errorf("unconfigured: want null label_ids, got %s", got.LabelIDs)
	}
	doc, _, d := provider.BuildUpdateCalendarEventBodyForTest(context.Background(), "5", got)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if doc.Data.Relationships.Labels != nil {
		t.Errorf("unconfigured: want labels omitted from the update, got %+v", doc.Data.Relationships.Labels)
	}

	got = provider.CalendarEventConfiguredLabelsForTest(plan, provider.CalendarEventResourceModelForTest{LabelIDs: npIDList("l1")})
	npAssertIDs(t, "configured label_ids", got.LabelIDs, "l1")
}

// ---------------------------------------------------------------------------
// Read query parameters
// ---------------------------------------------------------------------------

func TestCalendarEventReadParams_includesAndPagesLabels(t *testing.T) {
	req, err := api.NewGetCalendarEventRequest("https://api.example.com", "5", provider.CalendarEventReadParamsForTest())
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	if got := q.Get("include"); got != "labels" {
		t.Errorf("include: got %q", got)
	}
	if got := q.Get("page[size]"); got != "labels:500" {
		t.Errorf("page[size]: got %q (url %s)", got, req.URL)
	}
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

func TestCalendarEventContainsLetterValidator(t *testing.T) {
	v := provider.CalendarEventContainsLetterValidatorForTest()
	cases := map[string]bool{
		"Release":   true,
		"v1.2":      true,
		"123 - 456": false,
		"ü":         false, // the API's [a-zA-Z] is ASCII-only
	}
	for in, ok := range cases {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("title"), ConfigValue: types.StringValue(in)}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: want ok=%v, got diagnostics %v", in, ok, resp.Diagnostics)
		}
	}
	for _, v2 := range []types.String{types.StringNull(), types.StringUnknown()} {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("title"), ConfigValue: v2}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: want no error", v2)
		}
	}
}

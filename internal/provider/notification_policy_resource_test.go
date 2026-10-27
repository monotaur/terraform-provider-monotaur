package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// Fixtures and helpers
// ---------------------------------------------------------------------------

func notificationPolicyFixture() api.DataInNotificationPolicyResponse {
	name := "prod on-call"
	enabled := false
	severity := api.AlarmSeverityError
	repeat := int32(15)
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 13, 30, 0, 0, time.UTC)

	return api.DataInNotificationPolicyResponse{
		Id: "7",
		Attributes: &api.AttributesInNotificationPolicyResponse{
			Name:                  &name,
			Enabled:               &enabled,
			MinimumSeverity:       &severity,
			RepeatIntervalMinutes: &repeat,
			CreateDateTime:        &created,
			UpdateDateTime:        &updated,
		},
		Relationships: &api.RelationshipsInNotificationPolicyResponse{
			Channels: &api.ToManyNotificationChannelInResponse{
				Data: &[]api.NotificationChannelIdentifierInResponse{{Id: "ch1"}, {Id: "ch2"}},
			},
			Labels: &api.ToManyLabelInResponse{
				Data: &[]api.LabelIdentifierInResponse{{Id: "l1"}},
			},
			Components: &api.ToManyComponentInResponse{
				Data: &[]api.ComponentIdentifierInResponse{{Id: "c1"}},
			},
			Monitors: &api.ToManyMonitorInResponse{
				Data: &[]api.MonitorIdentifierInResponse{{Id: "m1"}, {Id: "m2"}, {Id: "m3"}},
			},
		},
	}
}

func npListIDs(t *testing.T, l types.List) []string {
	t.Helper()
	if l.IsNull() || l.IsUnknown() {
		t.Fatalf("expected known list, got null=%v unknown=%v", l.IsNull(), l.IsUnknown())
	}
	var elems []types.String
	if d := l.ElementsAs(context.Background(), &elems, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	out := make([]string, len(elems))
	for i, e := range elems {
		out[i] = e.ValueString()
	}
	return out
}

func npIDList(ids ...string) types.List {
	if ids == nil {
		ids = []string{} // ListValueFrom(nil) would yield a null list
	}
	l, d := types.ListValueFrom(context.Background(), types.StringType, ids)
	if d.HasError() {
		panic(d)
	}
	return l
}

func npAssertIDs(t *testing.T, field string, l types.List, want ...string) {
	t.Helper()
	got := npListIDs(t, l)
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: want %v, got %v", field, want, got)
	}
}

// ---------------------------------------------------------------------------
// flattenNotificationPolicy
// ---------------------------------------------------------------------------

func TestFlattenNotificationPolicy_allFields(t *testing.T) {
	var m provider.NotificationPolicyResourceModelForTest
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), notificationPolicyFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}

	if m.ID.ValueString() != "7" {
		t.Errorf("ID: got %q", m.ID.ValueString())
	}
	if m.Name.ValueString() != "prod on-call" {
		t.Errorf("Name: got %q", m.Name.ValueString())
	}
	if m.Enabled.IsNull() || m.Enabled.ValueBool() {
		t.Errorf("Enabled: want false, got %v", m.Enabled)
	}
	if m.MinimumSeverity.ValueString() != "Error" {
		t.Errorf("MinimumSeverity: got %q", m.MinimumSeverity.ValueString())
	}
	if m.RepeatIntervalMinutes.ValueInt64() != 15 {
		t.Errorf("RepeatIntervalMinutes: got %d", m.RepeatIntervalMinutes.ValueInt64())
	}
	if m.CreateDateTime.ValueString() != "2026-10-01T12:00:00Z" {
		t.Errorf("CreateDateTime: got %q", m.CreateDateTime.ValueString())
	}
	if m.UpdateDateTime.ValueString() != "2026-10-02T13:30:00Z" {
		t.Errorf("UpdateDateTime: got %q", m.UpdateDateTime.ValueString())
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "ch1", "ch2")
	npAssertIDs(t, "LabelIDs", m.LabelIDs, "l1")
	npAssertIDs(t, "ComponentIDs", m.ComponentIDs, "c1")
	npAssertIDs(t, "MonitorIDs", m.MonitorIDs, "m1", "m2", "m3")
}

func TestFlattenNotificationPolicy_nilOptionalAttributesAreNull(t *testing.T) {
	fx := notificationPolicyFixture()
	fx.Attributes.Enabled = nil
	fx.Attributes.MinimumSeverity = nil
	fx.Attributes.RepeatIntervalMinutes = nil
	fx.Attributes.CreateDateTime = nil
	fx.Attributes.UpdateDateTime = nil

	m := provider.NotificationPolicyResourceModelForTest{
		MinimumSeverity:       types.StringValue("Critical"),
		RepeatIntervalMinutes: types.Int64Value(30),
	}
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if !m.Enabled.IsNull() {
		t.Errorf("Enabled: want null, got %v", m.Enabled)
	}
	if !m.MinimumSeverity.IsNull() {
		t.Errorf("MinimumSeverity: want null, got %q", m.MinimumSeverity.ValueString())
	}
	if !m.RepeatIntervalMinutes.IsNull() {
		t.Errorf("RepeatIntervalMinutes: want null, got %d", m.RepeatIntervalMinutes.ValueInt64())
	}
	if !m.CreateDateTime.IsNull() || !m.UpdateDateTime.IsNull() {
		t.Errorf("timestamps: want null, got %v / %v", m.CreateDateTime, m.UpdateDateTime)
	}
}

func TestFlattenNotificationPolicy_nilRelationshipsWithNullPriorBecomeEmpty(t *testing.T) {
	fx := notificationPolicyFixture()
	fx.Relationships = nil

	var m provider.NotificationPolicyResourceModelForTest
	m.ChannelIDs = types.ListUnknown(types.StringType)
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs)
	npAssertIDs(t, "LabelIDs", m.LabelIDs)
	npAssertIDs(t, "ComponentIDs", m.ComponentIDs)
	npAssertIDs(t, "MonitorIDs", m.MonitorIDs)
}

func TestFlattenNotificationPolicy_linksOnlyPreservesPrior(t *testing.T) {
	fx := notificationPolicyFixture()
	// Relationships echoed with links only (no data), as on POST/PATCH responses.
	fx.Relationships = &api.RelationshipsInNotificationPolicyResponse{
		Channels:   &api.ToManyNotificationChannelInResponse{},
		Labels:     &api.ToManyLabelInResponse{},
		Components: &api.ToManyComponentInResponse{},
		Monitors:   &api.ToManyMonitorInResponse{},
	}

	m := provider.NotificationPolicyResourceModelForTest{
		ChannelIDs:   npIDList("ch9"),
		LabelIDs:     npIDList("l9", "l8"),
		ComponentIDs: npIDList(),
		MonitorIDs:   npIDList("m9"),
	}
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "ch9")
	npAssertIDs(t, "LabelIDs", m.LabelIDs, "l9", "l8")
	npAssertIDs(t, "ComponentIDs", m.ComponentIDs)
	npAssertIDs(t, "MonitorIDs", m.MonitorIDs, "m9")
}

func TestFlattenNotificationPolicy_eachRelationshipIndividually(t *testing.T) {
	cases := []struct {
		name  string
		rels  api.RelationshipsInNotificationPolicyResponse
		check func(t *testing.T, m provider.NotificationPolicyResourceModelForTest)
	}{
		{
			name: "channels",
			rels: api.RelationshipsInNotificationPolicyResponse{Channels: &api.ToManyNotificationChannelInResponse{
				Data: &[]api.NotificationChannelIdentifierInResponse{{Id: "a"}},
			}},
			check: func(t *testing.T, m provider.NotificationPolicyResourceModelForTest) {
				npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "a")
				npAssertIDs(t, "LabelIDs", m.LabelIDs)
			},
		},
		{
			name: "labels",
			rels: api.RelationshipsInNotificationPolicyResponse{Labels: &api.ToManyLabelInResponse{
				Data: &[]api.LabelIdentifierInResponse{{Id: "b"}},
			}},
			check: func(t *testing.T, m provider.NotificationPolicyResourceModelForTest) {
				npAssertIDs(t, "LabelIDs", m.LabelIDs, "b")
				npAssertIDs(t, "ChannelIDs", m.ChannelIDs)
			},
		},
		{
			name: "components",
			rels: api.RelationshipsInNotificationPolicyResponse{Components: &api.ToManyComponentInResponse{
				Data: &[]api.ComponentIdentifierInResponse{{Id: "c"}},
			}},
			check: func(t *testing.T, m provider.NotificationPolicyResourceModelForTest) {
				npAssertIDs(t, "ComponentIDs", m.ComponentIDs, "c")
				npAssertIDs(t, "MonitorIDs", m.MonitorIDs)
			},
		},
		{
			name: "monitors",
			rels: api.RelationshipsInNotificationPolicyResponse{Monitors: &api.ToManyMonitorInResponse{
				Data: &[]api.MonitorIdentifierInResponse{{Id: "d"}},
			}},
			check: func(t *testing.T, m provider.NotificationPolicyResourceModelForTest) {
				npAssertIDs(t, "MonitorIDs", m.MonitorIDs, "d")
				npAssertIDs(t, "ComponentIDs", m.ComponentIDs)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := notificationPolicyFixture()
			rels := tc.rels
			fx.Relationships = &rels
			var m provider.NotificationPolicyResourceModelForTest
			if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
				t.Fatalf("flatten: %v", d)
			}
			tc.check(t, m)
		})
	}
}

func TestFlattenNotificationPolicy_keepsPriorOrderForSameIDs(t *testing.T) {
	fx := notificationPolicyFixture() // API returns monitors m1, m2, m3
	m := provider.NotificationPolicyResourceModelForTest{MonitorIDs: npIDList("m3", "m1", "m2")}
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "MonitorIDs", m.MonitorIDs, "m3", "m1", "m2")
}

func TestFlattenNotificationPolicy_driftReplacesPrior(t *testing.T) {
	fx := notificationPolicyFixture() // API returns labels [l1]
	m := provider.NotificationPolicyResourceModelForTest{LabelIDs: npIDList("l1", "l2")}
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "LabelIDs", m.LabelIDs, "l1")
}

func TestFlattenNotificationPolicy_emptyDataClearsPrior(t *testing.T) {
	fx := notificationPolicyFixture()
	fx.Relationships.Labels = &api.ToManyLabelInResponse{Data: &[]api.LabelIdentifierInResponse{}}
	m := provider.NotificationPolicyResourceModelForTest{LabelIDs: npIDList("l1")}
	if d := provider.FlattenNotificationPolicyForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "LabelIDs", m.LabelIDs)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

func TestBuildCreateNotificationPolicyRelationships_nullAndUnknownOmitted(t *testing.T) {
	plan := provider.NotificationPolicyResourceModelForTest{
		ChannelIDs:   types.ListNull(types.StringType),
		LabelIDs:     types.ListUnknown(types.StringType),
		ComponentIDs: types.ListNull(types.StringType),
		MonitorIDs:   types.ListUnknown(types.StringType),
	}
	rels, d := provider.BuildCreateNotificationPolicyRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if rels.Channels != nil || rels.Labels != nil || rels.Components != nil || rels.Monitors != nil {
		t.Errorf("expected all relationships omitted, got %+v", rels)
	}
	if rels.EscalationSteps != nil {
		t.Error("escalationSteps must never be sent")
	}
	if rels.OpenapiDiscriminator != api.ResourceTypeNotificationPolicies {
		t.Errorf("discriminator: got %q", rels.OpenapiDiscriminator)
	}
}

func TestBuildCreateNotificationPolicyRelationships_populated(t *testing.T) {
	plan := provider.NotificationPolicyResourceModelForTest{
		ChannelIDs:   npIDList("ch1", "ch2"),
		LabelIDs:     npIDList("l1"),
		ComponentIDs: npIDList("c1"),
		MonitorIDs:   npIDList("m1"),
	}
	rels, d := provider.BuildCreateNotificationPolicyRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if rels.Channels == nil || len(rels.Channels.Data) != 2 ||
		rels.Channels.Data[1].Id != "ch2" || rels.Channels.Data[1].Type != api.ResourceTypeNotificationChannels {
		t.Errorf("Channels: got %+v", rels.Channels)
	}
	if rels.Labels == nil || len(rels.Labels.Data) != 1 || rels.Labels.Data[0].Type != api.ResourceTypeLabels {
		t.Errorf("Labels: got %+v", rels.Labels)
	}
	if rels.Components == nil || len(rels.Components.Data) != 1 || rels.Components.Data[0].Type != api.ResourceTypeComponents {
		t.Errorf("Components: got %+v", rels.Components)
	}
	if rels.Monitors == nil || len(rels.Monitors.Data) != 1 || rels.Monitors.Data[0].Type != api.ResourceTypeMonitors {
		t.Errorf("Monitors: got %+v", rels.Monitors)
	}
}

func TestBuildUpdateNotificationPolicyRelationships_emptyListSendsEmptyData(t *testing.T) {
	plan := provider.NotificationPolicyResourceModelForTest{
		ChannelIDs:   npIDList("ch1"),
		LabelIDs:     npIDList(),
		ComponentIDs: types.ListNull(types.StringType),
		MonitorIDs:   npIDList(),
	}
	rels, d := provider.BuildUpdateNotificationPolicyRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	b, err := json.Marshal(rels)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"labels":{"data":[]}`, `"monitors":{"data":[]}`, `"channels":{"data":[{"id":"ch1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in %s", want, s)
		}
	}
	for _, unwanted := range []string{`"components"`, `"escalationSteps"`} {
		if strings.Contains(s, unwanted) {
			t.Errorf("did not expect %s in %s", unwanted, s)
		}
	}
}

// ---------------------------------------------------------------------------
// Attribute builders and null handling
// ---------------------------------------------------------------------------

func TestBuildCreateNotificationPolicyAttributes(t *testing.T) {
	plan := provider.NotificationPolicyResourceModelForTest{
		Name:                  types.StringValue("p"),
		Enabled:               types.BoolUnknown(),
		MinimumSeverity:       types.StringValue("Warning"),
		RepeatIntervalMinutes: types.Int64Value(60),
	}
	a := provider.BuildCreateNotificationPolicyAttributesForTest(plan)
	if a.Name != "p" {
		t.Errorf("Name: got %q", a.Name)
	}
	if a.Enabled != nil {
		t.Errorf("Enabled: unknown must be omitted, got %v", *a.Enabled)
	}
	if a.MinimumSeverity == nil || *a.MinimumSeverity != api.AlarmSeverityWarning {
		t.Errorf("MinimumSeverity: got %v", a.MinimumSeverity)
	}
	if a.RepeatIntervalMinutes == nil || *a.RepeatIntervalMinutes != 60 {
		t.Errorf("RepeatIntervalMinutes: got %v", a.RepeatIntervalMinutes)
	}
}

func TestNotificationPolicyNullAttributes(t *testing.T) {
	cases := []struct {
		name string
		plan provider.NotificationPolicyResourceModelForTest
		want []string
	}{
		{
			name: "both set",
			plan: provider.NotificationPolicyResourceModelForTest{
				MinimumSeverity:       types.StringValue("Info"),
				RepeatIntervalMinutes: types.Int64Value(5),
			},
			want: nil,
		},
		{
			name: "both null",
			plan: provider.NotificationPolicyResourceModelForTest{
				MinimumSeverity:       types.StringNull(),
				RepeatIntervalMinutes: types.Int64Null(),
			},
			want: []string{"minimumSeverity", "repeatIntervalMinutes"},
		},
		{
			name: "only repeat null",
			plan: provider.NotificationPolicyResourceModelForTest{
				MinimumSeverity:       types.StringValue("Error"),
				RepeatIntervalMinutes: types.Int64Null(),
			},
			want: []string{"repeatIntervalMinutes"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := provider.NotificationPolicyNullAttributesForTest(tc.plan)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// TestNotificationPolicyUpdateBody_sendsExplicitNulls checks the full PATCH
// body the resource sends when minimum_severity and repeat_interval_minutes
// are removed from configuration.
func TestNotificationPolicyUpdateBody_sendsExplicitNulls(t *testing.T) {
	plan := provider.NotificationPolicyResourceModelForTest{
		Name:                  types.StringValue("p"),
		Enabled:               types.BoolValue(true),
		MinimumSeverity:       types.StringNull(),
		RepeatIntervalMinutes: types.Int64Null(),
		ChannelIDs:            npIDList(),
		LabelIDs:              npIDList(),
		ComponentIDs:          npIDList(),
		MonitorIDs:            npIDList(),
	}
	rels, d := provider.BuildUpdateNotificationPolicyRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	doc := api.UpdateNotificationPolicyRequestDocument{
		Data: api.DataInUpdateNotificationPolicyRequest{
			Id:            "7",
			Type:          api.ResourceTypeNotificationPolicies,
			Attributes:    provider.BuildUpdateNotificationPolicyAttributesForTest(plan),
			Relationships: rels,
		},
	}
	r, err := client.MarshalWithNullAttributes(doc, provider.NotificationPolicyNullAttributesForTest(plan))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)

	var decoded struct {
		Data struct {
			Attributes map[string]json.RawMessage `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	for _, k := range []string{"minimumSeverity", "repeatIntervalMinutes"} {
		v, ok := decoded.Data.Attributes[k]
		if !ok || string(v) != "null" {
			t.Errorf("attribute %s: want explicit null, got %q (present=%v) in %s", k, v, ok, b)
		}
	}
	if string(decoded.Data.Attributes["name"]) != `"p"` {
		t.Errorf("name: got %s", decoded.Data.Attributes["name"])
	}
	if string(decoded.Data.Attributes["enabled"]) != "true" {
		t.Errorf("enabled: got %s", decoded.Data.Attributes["enabled"])
	}
}

// ---------------------------------------------------------------------------
// Read query parameters
// ---------------------------------------------------------------------------

func TestNotificationPolicyReadParams_includesAndPagesRelationships(t *testing.T) {
	req, err := api.NewGetNotificationPolicyRequest("https://api.example.com", "7", provider.NotificationPolicyReadParamsForTest())
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	if got := q.Get("include"); got != "channels,labels,components,monitors" {
		t.Errorf("include: got %q", got)
	}
	if got := q.Get("page[size]"); got != "channels:500,labels:500,components:500,monitors:500" {
		t.Errorf("page[size]: got %q (url %s)", got, req.URL)
	}
}

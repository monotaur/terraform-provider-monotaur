package provider_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

func notificationEscalationStepFixture() api.DataInNotificationEscalationStepResponse {
	delay := int32(30)
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)
	return api.DataInNotificationEscalationStepResponse{
		Id: "11",
		Attributes: &api.AttributesInNotificationEscalationStepResponse{
			DelayMinutes:   &delay,
			CreateDateTime: &created,
			UpdateDateTime: &updated,
		},
		Relationships: &api.RelationshipsInNotificationEscalationStepResponse{
			NotificationPolicy: &api.ToOneNotificationPolicyInResponse{
				Data: &api.NotificationPolicyIdentifierInResponse{Id: "7"},
			},
			Channels: &api.ToManyNotificationChannelInResponse{
				Data: &[]api.NotificationChannelIdentifierInResponse{{Id: "ch1"}, {Id: "ch2"}},
			},
		},
	}
}

func TestFlattenNotificationEscalationStep_allFields(t *testing.T) {
	var m provider.NotificationEscalationStepResourceModelForTest
	if d := provider.FlattenNotificationEscalationStepForTest(context.Background(), notificationEscalationStepFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.ID.ValueString() != "11" {
		t.Errorf("ID: got %q", m.ID.ValueString())
	}
	if m.DelayMinutes.ValueInt64() != 30 {
		t.Errorf("DelayMinutes: got %d", m.DelayMinutes.ValueInt64())
	}
	if m.NotificationPolicyID.ValueString() != "7" {
		t.Errorf("NotificationPolicyID: got %q", m.NotificationPolicyID.ValueString())
	}
	if m.CreateDateTime.ValueString() != "2026-10-01T12:00:00Z" {
		t.Errorf("CreateDateTime: got %q", m.CreateDateTime.ValueString())
	}
	if m.UpdateDateTime.ValueString() != "2026-10-01T12:05:00Z" {
		t.Errorf("UpdateDateTime: got %q", m.UpdateDateTime.ValueString())
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "ch1", "ch2")
}

func TestFlattenNotificationEscalationStep_nilAttributesAreNull(t *testing.T) {
	fx := notificationEscalationStepFixture()
	fx.Attributes.DelayMinutes = nil
	fx.Attributes.CreateDateTime = nil
	fx.Attributes.UpdateDateTime = nil
	var m provider.NotificationEscalationStepResourceModelForTest
	if d := provider.FlattenNotificationEscalationStepForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if !m.DelayMinutes.IsNull() || !m.CreateDateTime.IsNull() || !m.UpdateDateTime.IsNull() {
		t.Errorf("expected nulls, got %v %v %v", m.DelayMinutes, m.CreateDateTime, m.UpdateDateTime)
	}
}

func TestFlattenNotificationEscalationStep_linksOnlyPreservesPrior(t *testing.T) {
	fx := notificationEscalationStepFixture()
	fx.Relationships = &api.RelationshipsInNotificationEscalationStepResponse{
		NotificationPolicy: &api.ToOneNotificationPolicyInResponse{},
		Channels:           &api.ToManyNotificationChannelInResponse{},
	}
	m := provider.NotificationEscalationStepResourceModelForTest{
		NotificationPolicyID: types.StringValue("99"),
		ChannelIDs:           npIDList("chX"),
	}
	if d := provider.FlattenNotificationEscalationStepForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if m.NotificationPolicyID.ValueString() != "99" {
		t.Errorf("NotificationPolicyID: want preserved 99, got %q", m.NotificationPolicyID.ValueString())
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "chX")
}

func TestFlattenNotificationEscalationStep_nilRelationshipsUnknownPrior(t *testing.T) {
	fx := notificationEscalationStepFixture()
	fx.Relationships = nil
	m := provider.NotificationEscalationStepResourceModelForTest{
		NotificationPolicyID: types.StringUnknown(),
		ChannelIDs:           types.ListUnknown(types.StringType),
	}
	if d := provider.FlattenNotificationEscalationStepForTest(context.Background(), fx, &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	if !m.NotificationPolicyID.IsNull() {
		t.Errorf("NotificationPolicyID: want null, got %v", m.NotificationPolicyID)
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs)
}

func TestFlattenNotificationEscalationStep_keepsPriorChannelOrder(t *testing.T) {
	m := provider.NotificationEscalationStepResourceModelForTest{ChannelIDs: npIDList("ch2", "ch1")}
	if d := provider.FlattenNotificationEscalationStepForTest(context.Background(), notificationEscalationStepFixture(), &m); d.HasError() {
		t.Fatalf("flatten: %v", d)
	}
	npAssertIDs(t, "ChannelIDs", m.ChannelIDs, "ch2", "ch1")
}

func TestBuildCreateNotificationEscalationStepRelationships(t *testing.T) {
	plan := provider.NotificationEscalationStepResourceModelForTest{
		NotificationPolicyID: types.StringValue("7"),
		ChannelIDs:           npIDList("ch1"),
	}
	rels, d := provider.BuildCreateNotificationEscalationStepRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if rels.NotificationPolicy.Data.Id != "7" || rels.NotificationPolicy.Data.Type != api.ResourceTypeNotificationPolicies {
		t.Errorf("NotificationPolicy: got %+v", rels.NotificationPolicy)
	}
	if rels.Channels == nil || len(rels.Channels.Data) != 1 || rels.Channels.Data[0].Id != "ch1" ||
		rels.Channels.Data[0].Type != api.ResourceTypeNotificationChannels {
		t.Errorf("Channels: got %+v", rels.Channels)
	}
}

func TestBuildUpdateNotificationEscalationStepRelationships_omitsPolicy(t *testing.T) {
	plan := provider.NotificationEscalationStepResourceModelForTest{
		NotificationPolicyID: types.StringValue("7"),
		ChannelIDs:           npIDList("ch1", "ch2"),
	}
	rels, d := provider.BuildUpdateNotificationEscalationStepRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if rels.NotificationPolicy != nil {
		t.Errorf("NotificationPolicy: must not be sent on update (attribute forces replacement), got %+v", rels.NotificationPolicy)
	}
	b, _ := json.Marshal(rels)
	if !strings.Contains(string(b), `"channels":{"data":[{"id":"ch1"`) {
		t.Errorf("channels missing from %s", b)
	}
}

func TestBuildUpdateNotificationEscalationStepRelationships_unknownChannelsOmitted(t *testing.T) {
	plan := provider.NotificationEscalationStepResourceModelForTest{
		NotificationPolicyID: types.StringValue("7"),
		ChannelIDs:           types.ListUnknown(types.StringType),
	}
	rels, d := provider.BuildUpdateNotificationEscalationStepRelationshipsForTest(context.Background(), plan)
	if d.HasError() {
		t.Fatalf("build: %v", d)
	}
	if rels.Channels != nil {
		t.Errorf("Channels: want omitted, got %+v", rels.Channels)
	}
}

func TestNotificationEscalationStepReadParams_includesRelationships(t *testing.T) {
	req, err := api.NewGetNotificationEscalationStepRequest("https://api.example.com", "11", provider.NotificationEscalationStepReadParamsForTest())
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	if got := q.Get("include"); got != "notificationPolicy,channels" {
		t.Errorf("include: got %q", got)
	}
	if got := q.Get("page[size]"); got != "channels:500" {
		t.Errorf("page[size]: got %q (url %s)", got, req.URL)
	}
}

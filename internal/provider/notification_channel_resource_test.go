package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenNotificationChannel unit tests
// ---------------------------------------------------------------------------

func notificationChannelFlattenFixture() api.DataInNotificationChannelResponse {
	name := "On-call email"
	channelType := api.Email
	enabled := true
	eventTypes := []api.WebhookEventType{api.ComMonotaurAlarmTriggered, api.ComMonotaurAlarmCleared}
	recipients := []string{"ops@example.com", "Ops <oncall@example.com>"}
	subject := "[Monotaur] {{ summary }}"
	body := "{{ summary }}"
	failures := int32(3)
	created := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	disabled := time.Date(2026, 10, 3, 11, 15, 0, 0, time.UTC)

	return api.DataInNotificationChannelResponse{
		Id: "7",
		Attributes: &api.AttributesInNotificationChannelResponse{
			Name:                    &name,
			Type:                    &channelType,
			Enabled:                 &enabled,
			EventTypes:              &eventTypes,
			Recipients:              &recipients,
			SubjectTemplate:         &subject,
			BodyTemplate:            &body,
			ConsecutiveFailureCount: &failures,
			CreateDateTime:          &created,
			UpdateDateTime:          &updated,
			AutoDisabledDateTime:    &disabled,
		},
	}
}

func setStrings(t *testing.T, s types.Set) []string {
	t.Helper()
	var out []string
	if d := s.ElementsAs(context.Background(), &out, false); d.HasError() {
		t.Fatalf("set ElementsAs: %v", d)
	}
	slices.Sort(out)
	return out
}

func listStrings(t *testing.T, l types.List) []string {
	t.Helper()
	out := []string{}
	if d := l.ElementsAs(context.Background(), &out, false); d.HasError() {
		t.Fatalf("list ElementsAs: %v", d)
	}
	return out
}

func TestFlattenNotificationChannel(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.DataInNotificationChannelResponse)
		prior  provider.NotificationChannelResourceModelForTest
		check  func(t *testing.T, m provider.NotificationChannelResourceModelForTest)
	}{
		{
			name: "all attributes set",
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if got := m.ID.ValueString(); got != "7" {
					t.Errorf("ID: want 7, got %q", got)
				}
				if got := m.Name.ValueString(); got != "On-call email" {
					t.Errorf("Name: got %q", got)
				}
				if got := m.Type.ValueString(); got != "Email" {
					t.Errorf("Type: got %q", got)
				}
				if !m.Enabled.ValueBool() {
					t.Error("Enabled: want true")
				}
				if got := setStrings(t, m.EventTypes); !slices.Equal(got, []string{"com.monotaur.alarm.cleared", "com.monotaur.alarm.triggered"}) {
					t.Errorf("EventTypes: got %v", got)
				}
				if got := listStrings(t, m.Recipients); !slices.Equal(got, []string{"ops@example.com", "Ops <oncall@example.com>"}) {
					t.Errorf("Recipients: got %v (order must be preserved)", got)
				}
				if got := m.SubjectTemplate.ValueString(); got != "[Monotaur] {{ summary }}" {
					t.Errorf("SubjectTemplate: got %q", got)
				}
				if got := m.BodyTemplate.ValueString(); got != "{{ summary }}" {
					t.Errorf("BodyTemplate: got %q", got)
				}
				if got := m.ConsecutiveFailureCount.ValueInt64(); got != 3 {
					t.Errorf("ConsecutiveFailureCount: got %d", got)
				}
				if got := m.CreateDateTime.ValueString(); got != "2026-10-01T09:30:00Z" {
					t.Errorf("CreateDateTime: got %q", got)
				}
				if got := m.UpdateDateTime.ValueString(); got != "2026-10-02T10:00:00Z" {
					t.Errorf("UpdateDateTime: got %q", got)
				}
				if got := m.AutoDisabledDateTime.ValueString(); got != "2026-10-03T11:15:00Z" {
					t.Errorf("AutoDisabledDateTime: got %q", got)
				}
				if !m.Credential.IsNull() {
					t.Errorf("Credential: want null with no prior value, got %q", m.Credential.ValueString())
				}
			},
		},
		{
			name: "nil optional attributes",
			mutate: func(d *api.DataInNotificationChannelResponse) {
				d.Attributes.SubjectTemplate = nil
				d.Attributes.BodyTemplate = nil
				d.Attributes.AutoDisabledDateTime = nil
				d.Attributes.Recipients = nil
				d.Attributes.ConsecutiveFailureCount = nil
				d.Attributes.Enabled = nil
			},
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if !m.SubjectTemplate.IsNull() || !m.BodyTemplate.IsNull() {
					t.Error("templates: want null")
				}
				if !m.AutoDisabledDateTime.IsNull() {
					t.Error("AutoDisabledDateTime: want null")
				}
				if m.Recipients.IsNull() || len(m.Recipients.Elements()) != 0 {
					t.Errorf("Recipients: want empty (not null) list, got %v", m.Recipients)
				}
				if m.ConsecutiveFailureCount.ValueInt64() != 0 {
					t.Error("ConsecutiveFailureCount: want 0")
				}
				if !m.Enabled.IsNull() {
					t.Error("Enabled: want null")
				}
			},
		},
		{
			name:   "nil attributes block",
			mutate: func(d *api.DataInNotificationChannelResponse) { d.Attributes = nil },
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if m.ID.ValueString() != "7" {
					t.Error("ID: want 7")
				}
				if !m.Name.IsNull() || !m.Type.IsNull() {
					t.Error("Name/Type: want null")
				}
				if m.EventTypes.IsNull() || len(m.EventTypes.Elements()) != 0 {
					t.Error("EventTypes: want empty set")
				}
			},
		},
		{
			name:  "credential kept from prior model",
			prior: provider.NotificationChannelResourceModelForTest{Credential: types.StringValue("https://hooks.example.com/secret")},
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if got := m.Credential.ValueString(); got != "https://hooks.example.com/secret" {
					t.Errorf("Credential: want prior value kept, got %q", got)
				}
			},
		},
		{
			name:  "unknown credential becomes null",
			prior: provider.NotificationChannelResourceModelForTest{Credential: types.StringUnknown()},
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if !m.Credential.IsNull() {
					t.Error("Credential: want null")
				}
			},
		},
		{
			name: "duplicate event types collapse in set",
			mutate: func(d *api.DataInNotificationChannelResponse) {
				ets := []api.WebhookEventType{api.ComMonotaurAlarmTriggered, api.ComMonotaurAlarmTriggered}
				d.Attributes.EventTypes = &ets
			},
			check: func(t *testing.T, m provider.NotificationChannelResourceModelForTest) {
				if got := setStrings(t, m.EventTypes); len(got) != 1 {
					t.Errorf("EventTypes: want 1 element, got %v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := notificationChannelFlattenFixture()
			if tc.mutate != nil {
				tc.mutate(&fixture)
			}
			model := tc.prior
			if diags := provider.FlattenNotificationChannelForTest(context.Background(), fixture, &model); diags.HasError() {
				t.Fatalf("FlattenNotificationChannelForTest returned errors: %v", diags)
			}
			tc.check(t, model)
		})
	}
}

// ---------------------------------------------------------------------------
// Request body unit tests
// ---------------------------------------------------------------------------

func ncStringSet(vals ...string) types.Set {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elems)
}

func ncStringList(vals ...string) types.List {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.ListValueMust(types.StringType, elems)
}

// ncBaseModel is a Webhook channel as it would be planned/stored.
func ncBaseModel() provider.NotificationChannelResourceModelForTest {
	return provider.NotificationChannelResourceModelForTest{
		ID:              types.StringValue("7"),
		Name:            types.StringValue("hook"),
		Type:            types.StringValue("Webhook"),
		EventTypes:      ncStringSet("com.monotaur.alarm.triggered"),
		Enabled:         types.BoolValue(true),
		Credential:      types.StringValue("https://example.com/hook"),
		Recipients:      ncStringList(),
		SubjectTemplate: types.StringNull(),
		BodyTemplate:    types.StringNull(),
	}
}

// decodeUpdateAttributes returns data.attributes of an encoded PATCH body as raw JSON.
func decodeUpdateAttributes(t *testing.T, r io.Reader) map[string]json.RawMessage {
	t.Helper()
	var doc struct {
		Data struct {
			ID         string                     `json:"id"`
			Type       string                     `json:"type"`
			Attributes map[string]json.RawMessage `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if doc.Data.ID != "7" || doc.Data.Type != "notificationChannels" {
		t.Errorf("data id/type: got %q/%q", doc.Data.ID, doc.Data.Type)
	}
	return doc.Data.Attributes
}

func TestBuildUpdateNotificationChannelBody(t *testing.T) {
	tests := []struct {
		name    string
		plan    func(*provider.NotificationChannelResourceModelForTest)
		state   func(*provider.NotificationChannelResourceModelForTest)
		want    map[string]string // attribute -> exact raw JSON
		absent  []string
		present []string
	}{
		{
			name: "null templates are sent as explicit null; unchanged credential omitted; type never sent",
			want: map[string]string{
				"subjectTemplate": "null",
				"bodyTemplate":    "null",
				"name":            `"hook"`,
				"enabled":         "true",
				"eventTypes":      `["com.monotaur.alarm.triggered"]`,
				"recipients":      "[]",
			},
			absent: []string{"credential", "type"},
		},
		{
			name: "set template is sent as value",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.BodyTemplate = types.StringValue(`{"s": {{ summary | object.to_json }}}`)
			},
			want: map[string]string{
				"bodyTemplate":    `"{\"s\": {{ summary | object.to_json }}}"`,
				"subjectTemplate": "null",
			},
		},
		{
			name: "unknown template is omitted",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.SubjectTemplate = types.StringUnknown()
			},
			absent: []string{"subjectTemplate"},
			want:   map[string]string{"bodyTemplate": "null"},
		},
		{
			name: "changed credential is sent",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.Credential = types.StringValue("https://example.com/hook2")
			},
			want: map[string]string{"credential": `"https://example.com/hook2"`},
		},
		{
			name: "credential sent after import (state has none)",
			state: func(m *provider.NotificationChannelResourceModelForTest) {
				m.Credential = types.StringNull()
			},
			want: map[string]string{"credential": `"https://example.com/hook"`},
		},
		{
			name: "null credential in plan is never sent as null",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.Credential = types.StringNull()
			},
			absent: []string{"credential"},
		},
		{
			name: "unknown enabled and recipients are omitted",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.Enabled = types.BoolUnknown()
				m.Recipients = types.ListUnknown(types.StringType)
			},
			absent: []string{"enabled", "recipients"},
		},
		{
			name: "recipients preserved in order",
			plan: func(m *provider.NotificationChannelResourceModelForTest) {
				m.Recipients = ncStringList("b@example.com", "a@example.com")
			},
			want: map[string]string{"recipients": `["b@example.com","a@example.com"]`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, state := ncBaseModel(), ncBaseModel()
			if tc.plan != nil {
				tc.plan(&plan)
			}
			if tc.state != nil {
				tc.state(&state)
			}
			body, diags := provider.BuildUpdateNotificationChannelBodyForTest(context.Background(), "7", plan, state)
			if diags.HasError() {
				t.Fatalf("BuildUpdateNotificationChannelBodyForTest returned errors: %v", diags)
			}
			attrs := decodeUpdateAttributes(t, body)
			for k, want := range tc.want {
				got, ok := attrs[k]
				if !ok {
					t.Errorf("%s: missing from body", k)
					continue
				}
				if string(got) != want {
					t.Errorf("%s: want %s, got %s", k, want, got)
				}
			}
			for _, k := range tc.absent {
				if v, ok := attrs[k]; ok {
					t.Errorf("%s: want absent, got %s", k, v)
				}
			}
		})
	}
}

func TestBuildCreateNotificationChannelBody(t *testing.T) {
	ctx := context.Background()

	t.Run("full webhook plan", func(t *testing.T) {
		plan := ncBaseModel()
		plan.BodyTemplate = types.StringValue(`{"s": 1}`)
		doc, diags := provider.BuildCreateNotificationChannelBodyForTest(ctx, plan)
		if diags.HasError() {
			t.Fatalf("errors: %v", diags)
		}
		a := doc.Data.Attributes
		if doc.Data.Type != api.ResourceTypeNotificationChannels {
			t.Errorf("data.type: got %q", doc.Data.Type)
		}
		if a.Type == nil || *a.Type != api.Webhook {
			t.Errorf("Type: got %v", a.Type)
		}
		if a.Credential == nil || *a.Credential != "https://example.com/hook" {
			t.Errorf("Credential: got %v", a.Credential)
		}
		if a.BodyTemplate == nil || *a.BodyTemplate != `{"s": 1}` {
			t.Errorf("BodyTemplate: got %v", a.BodyTemplate)
		}
		if a.SubjectTemplate != nil {
			t.Error("SubjectTemplate: want nil")
		}
		if !slices.Equal(a.EventTypes, []api.WebhookEventType{api.ComMonotaurAlarmTriggered}) {
			t.Errorf("EventTypes: got %v", a.EventTypes)
		}
	})

	t.Run("unknown optionals omitted, recipients sent as empty array", func(t *testing.T) {
		plan := ncBaseModel()
		plan.Enabled = types.BoolUnknown()
		plan.Recipients = types.ListUnknown(types.StringType)
		plan.Credential = types.StringNull()
		doc, diags := provider.BuildCreateNotificationChannelBodyForTest(ctx, plan)
		if diags.HasError() {
			t.Fatalf("errors: %v", diags)
		}
		a := doc.Data.Attributes
		if a.Enabled != nil || a.Credential != nil {
			t.Error("Enabled/Credential: want nil")
		}
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Data struct {
				Attributes map[string]json.RawMessage `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		if got := string(raw.Data.Attributes["recipients"]); got != "[]" {
			t.Errorf("recipients: want [], got %s", got)
		}
	})
}

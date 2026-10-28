package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// ---------------------------------------------------------------------------
// flattenWebhookSubscription unit tests
// ---------------------------------------------------------------------------

func webhookSubscriptionFlattenFixture() api.DataInWebhookSubscriptionResponse {
	url := "https://example.com/hook"
	eventTypes := []api.WebhookEventType{api.ComMonotaurAlarmTriggered, api.ComMonotaurAlarmAcknowledged}
	mode := api.ChangesOnly
	enabled := true
	legacy := true
	failures := int32(4)
	created := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	prevExpiry := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	disabled := time.Date(2026, 10, 4, 11, 15, 0, 0, time.UTC)

	return api.DataInWebhookSubscriptionResponse{
		Id: "12",
		Attributes: &api.AttributesInWebhookSubscriptionResponse{
			Url:                              &url,
			EventTypes:                       &eventTypes,
			PayloadMode:                      &mode,
			Enabled:                          &enabled,
			LegacySignatureEnabled:           &legacy,
			ConsecutiveFailureCount:          &failures,
			CreateDateTime:                   &created,
			UpdateDateTime:                   &updated,
			PreviousSigningKeyExpiryDateTime: &prevExpiry,
			AutoDisabledDateTime:             &disabled,
		},
	}
}

func wsSetStrings(t *testing.T, s types.Set) []string {
	t.Helper()
	var out []string
	if d := s.ElementsAs(context.Background(), &out, false); d.HasError() {
		t.Fatalf("set ElementsAs: %v", d)
	}
	slices.Sort(out)
	return out
}

func TestFlattenWebhookSubscription(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.DataInWebhookSubscriptionResponse)
		prior  provider.WebhookSubscriptionResourceModelForTest
		check  func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest)
	}{
		{
			name: "all attributes set",
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if got := m.ID.ValueString(); got != "12" {
					t.Errorf("ID: want 12, got %q", got)
				}
				if got := m.URL.ValueString(); got != "https://example.com/hook" {
					t.Errorf("URL: got %q", got)
				}
				if got := wsSetStrings(t, m.EventTypes); !slices.Equal(got, []string{"com.monotaur.alarm.acknowledged", "com.monotaur.alarm.triggered"}) {
					t.Errorf("EventTypes: got %v", got)
				}
				if got := m.PayloadMode.ValueString(); got != "ChangesOnly" {
					t.Errorf("PayloadMode: got %q", got)
				}
				if !m.Enabled.ValueBool() {
					t.Error("Enabled: want true")
				}
				if !m.LegacySignatureEnabled.ValueBool() {
					t.Error("LegacySignatureEnabled: want true")
				}
				if got := m.ConsecutiveFailureCount.ValueInt64(); got != 4 {
					t.Errorf("ConsecutiveFailureCount: got %d", got)
				}
				if got := m.CreateDateTime.ValueString(); got != "2026-10-01T09:30:00Z" {
					t.Errorf("CreateDateTime: got %q", got)
				}
				if got := m.UpdateDateTime.ValueString(); got != "2026-10-02T10:00:00Z" {
					t.Errorf("UpdateDateTime: got %q", got)
				}
				if got := m.PreviousSigningKeyExpiryDateTime.ValueString(); got != "2026-10-03T10:00:00Z" {
					t.Errorf("PreviousSigningKeyExpiryDateTime: got %q", got)
				}
				if got := m.AutoDisabledDateTime.ValueString(); got != "2026-10-04T11:15:00Z" {
					t.Errorf("AutoDisabledDateTime: got %q", got)
				}
				if !m.SigningKey.IsNull() || !m.GeneratedSigningKey.IsNull() {
					t.Error("signing keys: want null with no prior value")
				}
			},
		},
		{
			name: "nil optional attributes",
			mutate: func(d *api.DataInWebhookSubscriptionResponse) {
				d.Attributes.PreviousSigningKeyExpiryDateTime = nil
				d.Attributes.AutoDisabledDateTime = nil
				d.Attributes.ConsecutiveFailureCount = nil
				d.Attributes.Enabled = nil
				d.Attributes.LegacySignatureEnabled = nil
				d.Attributes.PayloadMode = nil
				d.Attributes.EventTypes = nil
			},
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if !m.PreviousSigningKeyExpiryDateTime.IsNull() || !m.AutoDisabledDateTime.IsNull() {
					t.Error("timestamps: want null")
				}
				if m.ConsecutiveFailureCount.ValueInt64() != 0 {
					t.Error("ConsecutiveFailureCount: want 0")
				}
				if !m.Enabled.IsNull() || !m.LegacySignatureEnabled.IsNull() || !m.PayloadMode.IsNull() {
					t.Error("Enabled/LegacySignatureEnabled/PayloadMode: want null")
				}
				if m.EventTypes.IsNull() || len(m.EventTypes.Elements()) != 0 {
					t.Error("EventTypes: want empty (not null) set")
				}
			},
		},
		{
			name:   "nil attributes block",
			mutate: func(d *api.DataInWebhookSubscriptionResponse) { d.Attributes = nil },
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if m.ID.ValueString() != "12" {
					t.Error("ID: want 12")
				}
				if !m.URL.IsNull() {
					t.Error("URL: want null")
				}
				if m.EventTypes.IsNull() || len(m.EventTypes.Elements()) != 0 {
					t.Error("EventTypes: want empty set")
				}
			},
		},
		{
			name: "signing keys kept from prior model",
			prior: provider.WebhookSubscriptionResourceModelForTest{
				SigningKey:          types.StringValue("configured-key-configured-key-0123"),
				GeneratedSigningKey: types.StringValue("generated"),
			},
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if got := m.SigningKey.ValueString(); got != "configured-key-configured-key-0123" {
					t.Errorf("SigningKey: want prior value kept, got %q", got)
				}
				if got := m.GeneratedSigningKey.ValueString(); got != "generated" {
					t.Errorf("GeneratedSigningKey: want prior value kept, got %q", got)
				}
			},
		},
		{
			name: "unknown signing keys become null",
			prior: provider.WebhookSubscriptionResourceModelForTest{
				SigningKey:          types.StringUnknown(),
				GeneratedSigningKey: types.StringUnknown(),
			},
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if !m.SigningKey.IsNull() || !m.GeneratedSigningKey.IsNull() {
					t.Error("signing keys: want null")
				}
			},
		},
		{
			name: "duplicate event types collapse in set",
			mutate: func(d *api.DataInWebhookSubscriptionResponse) {
				ets := []api.WebhookEventType{api.ComMonotaurAlarmTriggered, api.ComMonotaurAlarmTriggered}
				d.Attributes.EventTypes = &ets
			},
			check: func(t *testing.T, m provider.WebhookSubscriptionResourceModelForTest) {
				if got := wsSetStrings(t, m.EventTypes); len(got) != 1 {
					t.Errorf("EventTypes: want 1 element, got %v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := webhookSubscriptionFlattenFixture()
			if tc.mutate != nil {
				tc.mutate(&fixture)
			}
			model := tc.prior
			if diags := provider.FlattenWebhookSubscriptionForTest(context.Background(), fixture, &model); diags.HasError() {
				t.Fatalf("FlattenWebhookSubscriptionForTest returned errors: %v", diags)
			}
			tc.check(t, model)
		})
	}
}

// ---------------------------------------------------------------------------
// meta.signingKey
// ---------------------------------------------------------------------------

func TestWebhookSubscriptionMetaSigningKey(t *testing.T) {
	ptr := func(v interface{}) *interface{} { return &v }

	tests := []struct {
		name   string
		meta   *api.Meta
		want   string
		wantOK bool
	}{
		{name: "nil meta"},
		{name: "no key", meta: &api.Meta{"other": ptr("x")}},
		{name: "null key", meta: &api.Meta{"signingKey": nil}},
		{name: "non-string key", meta: &api.Meta{"signingKey": ptr(float64(1))}},
		{name: "empty key", meta: &api.Meta{"signingKey": ptr("")}},
		{name: "key", meta: &api.Meta{"signingKey": ptr("abc")}, want: "abc", wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := provider.WebhookSubscriptionMetaSigningKeyForTest(tc.meta)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("got (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}

	// The key arrives in data.meta of the create response, which UnmarshalDocument decodes.
	t.Run("decoded from response", func(t *testing.T) {
		var data api.DataInWebhookSubscriptionResponse
		if err := json.Unmarshal([]byte(`{"type":"webhookSubscriptions","id":"1","attributes":{},"meta":{"signingKey":"gen"}}`), &data); err != nil {
			t.Fatal(err)
		}
		if got, ok := provider.WebhookSubscriptionMetaSigningKeyForTest(data.Meta); !ok || got != "gen" {
			t.Errorf("got (%q, %v)", got, ok)
		}
	})
}

// ---------------------------------------------------------------------------
// Request body unit tests
// ---------------------------------------------------------------------------

const wsTestKey = "0123456789abcdef0123456789abcdef"

func wsStringSet(vals ...string) types.Set {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elems)
}

// wsBaseModel is a subscription as it would be planned/stored.
func wsBaseModel() provider.WebhookSubscriptionResourceModelForTest {
	return provider.WebhookSubscriptionResourceModelForTest{
		ID:                     types.StringValue("12"),
		URL:                    types.StringValue("https://example.com/hook"),
		EventTypes:             wsStringSet("com.monotaur.alarm.triggered"),
		PayloadMode:            types.StringValue("Full"),
		Enabled:                types.BoolValue(true),
		SigningKey:             types.StringValue(wsTestKey),
		GeneratedSigningKey:    types.StringNull(),
		LegacySignatureEnabled: types.BoolValue(false),
	}
}

// wsDecodeUpdateAttributes returns data.attributes of an encoded PATCH body as raw JSON.
func wsDecodeUpdateAttributes(t *testing.T, r io.Reader) map[string]json.RawMessage {
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
	if doc.Data.ID != "12" || doc.Data.Type != "webhookSubscriptions" {
		t.Errorf("data id/type: got %q/%q", doc.Data.ID, doc.Data.Type)
	}
	return doc.Data.Attributes
}

func TestBuildUpdateWebhookSubscriptionBody(t *testing.T) {
	tests := []struct {
		name   string
		plan   func(*provider.WebhookSubscriptionResourceModelForTest)
		state  func(*provider.WebhookSubscriptionResourceModelForTest)
		want   map[string]string // attribute -> exact raw JSON
		absent []string
	}{
		{
			name: "known values sent; unchanged signing key omitted; no explicit nulls",
			want: map[string]string{
				"url":                    `"https://example.com/hook"`,
				"eventTypes":             `["com.monotaur.alarm.triggered"]`,
				"payloadMode":            `"Full"`,
				"enabled":                "true",
				"legacySignatureEnabled": "false",
			},
			absent: []string{"signingKey"},
		},
		{
			name: "changed signing key is sent",
			plan: func(m *provider.WebhookSubscriptionResourceModelForTest) {
				m.SigningKey = types.StringValue(wsTestKey + "-2")
			},
			want: map[string]string{"signingKey": `"` + wsTestKey + `-2"`},
		},
		{
			name: "signing key sent after import (state has none)",
			state: func(m *provider.WebhookSubscriptionResourceModelForTest) {
				m.SigningKey = types.StringNull()
			},
			want: map[string]string{"signingKey": `"` + wsTestKey + `"`},
		},
		{
			name: "null signing key in plan is never sent",
			plan: func(m *provider.WebhookSubscriptionResourceModelForTest) {
				m.SigningKey = types.StringNull()
			},
			absent: []string{"signingKey"},
		},
		{
			name: "unknown optionals are omitted",
			plan: func(m *provider.WebhookSubscriptionResourceModelForTest) {
				m.PayloadMode = types.StringUnknown()
				m.Enabled = types.BoolUnknown()
				m.LegacySignatureEnabled = types.BoolUnknown()
				m.SigningKey = types.StringUnknown()
			},
			absent: []string{"payloadMode", "enabled", "legacySignatureEnabled", "signingKey"},
			want:   map[string]string{"url": `"https://example.com/hook"`},
		},
		{
			name: "event types and changed values",
			plan: func(m *provider.WebhookSubscriptionResourceModelForTest) {
				m.EventTypes = wsStringSet("com.monotaur.alarm.cleared")
				m.PayloadMode = types.StringValue("ChangesOnly")
				m.Enabled = types.BoolValue(false)
				m.LegacySignatureEnabled = types.BoolValue(true)
			},
			want: map[string]string{
				"eventTypes":             `["com.monotaur.alarm.cleared"]`,
				"payloadMode":            `"ChangesOnly"`,
				"enabled":                "false",
				"legacySignatureEnabled": "true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, state := wsBaseModel(), wsBaseModel()
			if tc.plan != nil {
				tc.plan(&plan)
			}
			if tc.state != nil {
				tc.state(&state)
			}
			body, diags := provider.BuildUpdateWebhookSubscriptionBodyForTest(context.Background(), "12", plan, state)
			if diags.HasError() {
				t.Fatalf("BuildUpdateWebhookSubscriptionBodyForTest returned errors: %v", diags)
			}
			attrs := wsDecodeUpdateAttributes(t, body)
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
			for k, v := range attrs {
				if string(v) == "null" {
					t.Errorf("%s: unexpected explicit null", k)
				}
			}
		})
	}
}

func TestBuildCreateWebhookSubscriptionBody(t *testing.T) {
	ctx := context.Background()

	t.Run("full plan", func(t *testing.T) {
		plan := wsBaseModel()
		plan.PayloadMode = types.StringValue("ChangesOnly")
		plan.LegacySignatureEnabled = types.BoolValue(true)
		doc, diags := provider.BuildCreateWebhookSubscriptionBodyForTest(ctx, plan)
		if diags.HasError() {
			t.Fatalf("errors: %v", diags)
		}
		if doc.Data.Type != api.ResourceTypeWebhookSubscriptions {
			t.Errorf("data.type: got %q", doc.Data.Type)
		}
		a := doc.Data.Attributes
		if a.Url != "https://example.com/hook" {
			t.Errorf("Url: got %q", a.Url)
		}
		if !slices.Equal(a.EventTypes, []api.WebhookEventType{api.ComMonotaurAlarmTriggered}) {
			t.Errorf("EventTypes: got %v", a.EventTypes)
		}
		if a.PayloadMode == nil || *a.PayloadMode != api.ChangesOnly {
			t.Errorf("PayloadMode: got %v", a.PayloadMode)
		}
		if a.Enabled == nil || !*a.Enabled {
			t.Errorf("Enabled: got %v", a.Enabled)
		}
		if a.LegacySignatureEnabled == nil || !*a.LegacySignatureEnabled {
			t.Errorf("LegacySignatureEnabled: got %v", a.LegacySignatureEnabled)
		}
		if a.SigningKey == nil || *a.SigningKey != wsTestKey {
			t.Errorf("SigningKey: got %v", a.SigningKey)
		}
	})

	t.Run("unknown and null optionals omitted so the server applies defaults", func(t *testing.T) {
		plan := wsBaseModel()
		plan.PayloadMode = types.StringUnknown()
		plan.Enabled = types.BoolUnknown()
		plan.LegacySignatureEnabled = types.BoolUnknown()
		plan.SigningKey = types.StringNull()
		doc, diags := provider.BuildCreateWebhookSubscriptionBodyForTest(ctx, plan)
		if diags.HasError() {
			t.Fatalf("errors: %v", diags)
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
		for _, k := range []string{"payloadMode", "enabled", "legacySignatureEnabled", "signingKey"} {
			if v, ok := raw.Data.Attributes[k]; ok {
				t.Errorf("%s: want absent, got %s", k, v)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

func TestWebhookSubscriptionValidators(t *testing.T) {
	urlV, keyV := provider.WebhookSubscriptionValidatorsForTest()
	run := func(v validator.String, val types.String) bool {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: val}, resp)
		return !resp.Diagnostics.HasError()
	}

	urls := []struct {
		v  string
		ok bool
	}{
		{"https://example.com/hook", true},
		{"HTTPS://example.com", true},
		{"https://example.com:8443/a?b=c", true},
		{"http://example.com/hook", false},
		{"example.com/hook", false},
		{"https://", false},
		{"https:///path", false},
		{"ftp://example.com", false},
		{"https://example.com/" + strings.Repeat("a", 2048), false},
	}
	for _, tc := range urls {
		if got := run(urlV, types.StringValue(tc.v)); got != tc.ok {
			t.Errorf("url %q: valid=%v, want %v", tc.v, got, tc.ok)
		}
	}
	if !run(urlV, types.StringUnknown()) || !run(urlV, types.StringNull()) {
		t.Error("url: unknown/null must pass")
	}

	keys := []struct {
		v  string
		ok bool
	}{
		{strings.Repeat("a", 31), false},
		{strings.Repeat("a", 32), true},
		// 16 two-byte runes = 32 bytes of UTF-8: the server counts bytes.
		{strings.Repeat("é", 16), true},
		{strings.Repeat("é", 15) + "a", false},
	}
	for _, tc := range keys {
		if got := run(keyV, types.StringValue(tc.v)); got != tc.ok {
			t.Errorf("signing key of %d bytes: valid=%v, want %v", len(tc.v), got, tc.ok)
		}
	}
	if !run(keyV, types.StringUnknown()) || !run(keyV, types.StringNull()) {
		t.Error("signing key: unknown/null must pass")
	}
}

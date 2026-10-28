package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &webhookSubscriptionResource{}
	_ resource.ResourceWithConfigure   = &webhookSubscriptionResource{}
	_ resource.ResourceWithImportState = &webhookSubscriptionResource{}
)

// Server-side limits (Monotaur.Core WebhookSubscription / WebhookSubscriptionResourceService).
const (
	webhookSubscriptionMaxURLLength = 2048
	// webhookSubscriptionMinSigningKeyBytes is MinimumSigningKeyBytes: a supplied
	// key must be at least this many bytes of UTF-8 (ADR-0050).
	webhookSubscriptionMinSigningKeyBytes = 32
)

// webhookSubscriptionPayloadModes are the values of api.PayloadMode.
var webhookSubscriptionPayloadModes = []string{
	string(api.Full),
	string(api.ChangesOnly),
}

// webhookSubscriptionEventTypes are the CloudEvent types a subscription may
// take: every api.WebhookEventType (WebhookEventTypes.All on the server).
// Legacy internal event names (e.g. Resource.Alarm.Create) are not accepted:
// the server rewrites them to these CloudEvent names, which would surface as
// an inconsistent result after apply.
var webhookSubscriptionEventTypes = []string{
	string(api.ComMonotaurAlarmAcknowledged),
	string(api.ComMonotaurAlarmCleared),
	string(api.ComMonotaurAlarmNoteAdded),
	string(api.ComMonotaurAlarmTriggered),
	string(api.ComMonotaurComponentStatusChanged),
	string(api.ComMonotaurMaintenanceWindowStarted),
	string(api.ComMonotaurMaintenanceWindowStopped),
	string(api.ComMonotaurMaintenanceWindowUpcoming),
	string(api.ComMonotaurMonitorCreated),
	string(api.ComMonotaurMonitorDeleted),
	string(api.ComMonotaurMonitorHeartbeatMissed),
	string(api.ComMonotaurMonitorNoData),
	string(api.ComMonotaurMonitorStatusChanged),
	string(api.ComMonotaurNotificationChannelAutoDisabled),
	string(api.ComMonotaurProbeExecutionFailed),
	string(api.ComMonotaurWebhookSubscriptionAutoDisabled),
}

// NewWebhookSubscriptionResource returns a new webhookSubscriptionResource constructor function.
func NewWebhookSubscriptionResource() resource.Resource {
	return &webhookSubscriptionResource{}
}

// webhookSubscriptionResource implements the monotaur_webhook_subscription managed resource.
type webhookSubscriptionResource struct {
	client *client.Client
}

// webhookSubscriptionResourceModel is the Terraform state model for a webhook subscription.
//
// Attribute mapping:
//
//	AttributesInCreateWebhookSubscriptionRequest: url, eventTypes (required); payloadMode, enabled,
//	                                              signingKey, legacySignatureEnabled (optional)
//	AttributesInUpdateWebhookSubscriptionRequest: as create, all optional
//	AttributesInWebhookSubscriptionResponse:      all of the above except signingKey (write-only,
//	                                              never returned), plus createDateTime,
//	                                              updateDateTime, previousSigningKeyExpiryDateTime,
//	                                              consecutiveFailureCount, autoDisabledDateTime
//	DataInWebhookSubscriptionResponse.Meta:       signingKey, only on the create response and only
//	                                              when the request had none (generated_signing_key)
//
// There are no relationships.
type webhookSubscriptionResourceModel struct {
	ID                               types.String `tfsdk:"id"`
	URL                              types.String `tfsdk:"url"`
	EventTypes                       types.Set    `tfsdk:"event_types"`
	PayloadMode                      types.String `tfsdk:"payload_mode"`
	Enabled                          types.Bool   `tfsdk:"enabled"`
	SigningKey                       types.String `tfsdk:"signing_key"`
	GeneratedSigningKey              types.String `tfsdk:"generated_signing_key"`
	LegacySignatureEnabled           types.Bool   `tfsdk:"legacy_signature_enabled"`
	CreateDateTime                   types.String `tfsdk:"create_date_time"`
	UpdateDateTime                   types.String `tfsdk:"update_date_time"`
	PreviousSigningKeyExpiryDateTime types.String `tfsdk:"previous_signing_key_expiry_date_time"`
	ConsecutiveFailureCount          types.Int64  `tfsdk:"consecutive_failure_count"`
	AutoDisabledDateTime             types.String `tfsdk:"auto_disabled_date_time"`
}

// Metadata sets the resource type name.
func (r *webhookSubscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_subscription"
}

// Schema defines the Terraform schema for the resource.
func (r *webhookSubscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur webhook subscription. Monotaur POSTs each event the subscription " +
			"subscribes to as a CloudEvent to `url`, signed with HMAC-SHA256 using the subscription's signing key " +
			"(`X-Monotaur-Webhook-Signature: v1=<hex>` over `<timestamp>.<id>.<body>`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the webhook subscription (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "The absolute `https` URL deliveries are POSTed to (at most 2,048 characters). " +
					"The server also refuses hosts blocked by its webhook egress policy (e.g. private or loopback addresses).",
				Required: true,
				Validators: []validator.String{
					wsHTTPSURL(webhookSubscriptionMaxURLLength),
				},
			},
			"event_types": schema.SetAttribute{
				MarkdownDescription: "The CloudEvent types delivered to the subscription (at least one), e.g. " +
					"`com.monotaur.alarm.triggered`. Legacy internal event names are not accepted.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					wsSetSizeAtLeast(1),
					wsSetElementsOneOf(webhookSubscriptionEventTypes...),
				},
			},
			"payload_mode": schema.StringAttribute{
				MarkdownDescription: "How much of the resource a delivery carries: `Full` (the whole resource) or " +
					"`ChangesOnly`. Defaults to `Full` on the server.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					wsStringOneOf(webhookSubscriptionPayloadModes...),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether events are delivered to the subscription. Defaults to `true` on the server. " +
					"Delivery sets it to `false` after too many failed deliveries in a row; when this attribute is set " +
					"to `true` in the configuration the next apply enables it again, which also resets " +
					"`consecutive_failure_count` and `auto_disabled_date_time`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"signing_key": schema.StringAttribute{
				MarkdownDescription: "The HMAC-SHA256 signing key, at least 32 bytes of UTF-8 (e.g. the output of " +
					"`openssl rand -base64 32`). When omitted on create, the server generates one and it is exposed " +
					"once as `generated_signing_key`. Sensitive and write-only: the API never returns it, so the value " +
					"from the configuration is kept in state, drift (e.g. a rotation outside Terraform) is not detected, " +
					"and `terraform import` cannot recover it (the next apply re-sends the configured value). Changing " +
					"it replaces the key at once and ends any rotation overlap. Removing it from the configuration does " +
					"not change the key on the server.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					wsStringMinBytes(webhookSubscriptionMinSigningKeyBytes),
				},
			},
			"generated_signing_key": schema.StringAttribute{
				MarkdownDescription: "The signing key the server generated because `signing_key` was not set when the " +
					"subscription was created. The API returns it only in the create response, so it is null after " +
					"`terraform import`, and it is not updated by a `rotate-signing-key` call made outside Terraform. " +
					"Null whenever `signing_key` is set.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					wsGeneratedSigningKeyPlanModifier{},
				},
			},
			"legacy_signature_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether deliveries also carry the deprecated body-only `X-Monotaur-Signature-256` " +
					"header, which does not protect receivers against replay. Defaults to `false` on the server for new " +
					"subscriptions.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the subscription was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the subscription was last updated (assigned by the API).",
				Computed:            true,
			},
			"previous_signing_key_expiry_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp until which deliveries are also signed with the key replaced " +
					"by the last `rotate-signing-key` call, or null outside a rotation overlap. Changing `signing_key` " +
					"clears it (system-managed, read-only).",
				Computed: true,
			},
			"consecutive_failure_count": schema.Int64Attribute{
				MarkdownDescription: "Deliveries in a row that ended `Failed` (system-managed, read-only).",
				Computed:            true,
			},
			"auto_disabled_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when delivery disabled the subscription after too many " +
					"failures, or null (system-managed, read-only).",
				Computed: true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *webhookSubscriptionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = c
}

// Create creates a new webhook subscription via POST /webhookSubscriptions.
func (r *webhookSubscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookSubscriptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildCreateWebhookSubscriptionBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_webhook_subscription: creating webhook subscription")

	apiResp, err := r.client.Inner().PostWebhookSubscriptionWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostWebhookSubscriptionParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Webhook Subscription", "Could not create webhook subscription: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Webhook Subscription", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInWebhookSubscriptionResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenWebhookSubscription(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A generated key is returned once, in the created resource's meta.
	plan.GeneratedSigningKey = types.StringNull()
	if plan.SigningKey.IsNull() {
		if key, ok := webhookSubscriptionMetaSigningKey(data.Meta); ok {
			plan.GeneratedSigningKey = types.StringValue(key)
		} else {
			resp.Diagnostics.AddWarning("Generated Signing Key Not Returned",
				"The API did not return the signing key it generated for webhook subscription "+plan.ID.ValueString()+
					"; generated_signing_key is null. Set signing_key, or rotate the key outside Terraform, to obtain a key.")
		}
	}

	tflog.Debug(ctx, "monotaur_webhook_subscription: created webhook subscription", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *webhookSubscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookSubscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_webhook_subscription: reading webhook subscription", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetWebhookSubscription(ctx, id, &api.GetWebhookSubscriptionParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription", "Could not read webhook subscription "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_webhook_subscription: webhook subscription not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Webhook Subscription", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInWebhookSubscriptionResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription Response", err.Error())
		return
	}

	// Pass the existing state so flattenWebhookSubscription keeps the
	// write-only signing_key and generated_signing_key.
	resp.Diagnostics.Append(flattenWebhookSubscription(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing webhook subscription via PATCH /webhookSubscriptions/{id}.
func (r *webhookSubscriptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan webhookSubscriptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state webhookSubscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	reqBody, diags := buildUpdateWebhookSubscriptionBody(ctx, id, plan, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_webhook_subscription: updating webhook subscription", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchWebhookSubscriptionWithBody(ctx, id, &api.PatchWebhookSubscriptionParams{}, client.ContentType, reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Webhook Subscription", "Could not update webhook subscription "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Webhook Subscription", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetWebhookSubscription(ctx, id, &api.GetWebhookSubscriptionParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInWebhookSubscriptionResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription Response", err.Error())
		return
	}

	// generated_signing_key is unknown only when signing_key was unknown at
	// plan time (see wsGeneratedSigningKeyPlanModifier); resolve it now.
	if plan.GeneratedSigningKey.IsUnknown() {
		if wsKnownString(plan.SigningKey) {
			plan.GeneratedSigningKey = types.StringNull()
		} else {
			plan.GeneratedSigningKey = state.GeneratedSigningKey
		}
	}

	// Pass plan so flattenWebhookSubscription keeps the planned signing_key.
	resp.Diagnostics.Append(flattenWebhookSubscription(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_webhook_subscription: updated webhook subscription", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a webhook subscription via DELETE /webhookSubscriptions/{id}.
func (r *webhookSubscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookSubscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_webhook_subscription: deleting webhook subscription", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteWebhookSubscription(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Webhook Subscription", "Could not delete webhook subscription "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_webhook_subscription: webhook subscription already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Webhook Subscription", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_webhook_subscription: deleted webhook subscription", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_webhook_subscription.example <id>`.
// Neither signing_key nor generated_signing_key can be imported: the API never returns them.
func (r *webhookSubscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request builders
// ---------------------------------------------------------------------------

// webhookSubscriptionEventTypesFromSet converts the event_types set into the API enum slice.
func webhookSubscriptionEventTypesFromSet(ctx context.Context, set types.Set) ([]api.WebhookEventType, diag.Diagnostics) {
	var values []string
	diags := set.ElementsAs(ctx, &values, false)
	out := make([]api.WebhookEventType, len(values))
	for i, v := range values {
		out[i] = api.WebhookEventType(v)
	}
	return out, diags
}

// wsKnownString reports whether v holds a concrete (non-null, non-unknown) value.
func wsKnownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

// wsKnownBool reports whether v holds a concrete (non-null, non-unknown) value.
func wsKnownBool(v types.Bool) bool { return !v.IsNull() && !v.IsUnknown() }

// buildCreateWebhookSubscriptionBody builds the POST body from the plan.
// Optional attributes that are null or unknown are omitted, so the server
// applies its defaults (and generates a signing key when none is set).
func buildCreateWebhookSubscriptionBody(ctx context.Context, plan webhookSubscriptionResourceModel) (api.CreateWebhookSubscriptionRequestDocument, diag.Diagnostics) {
	attrs := &api.AttributesInCreateWebhookSubscriptionRequest{
		OpenapiDiscriminator: api.ResourceTypeWebhookSubscriptions,
		Url:                  plan.URL.ValueString(),
	}

	eventTypes, diags := webhookSubscriptionEventTypesFromSet(ctx, plan.EventTypes)
	attrs.EventTypes = eventTypes

	if wsKnownString(plan.PayloadMode) {
		v := api.PayloadMode(plan.PayloadMode.ValueString())
		attrs.PayloadMode = &v
	}
	if wsKnownBool(plan.Enabled) {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}
	if wsKnownString(plan.SigningKey) {
		v := plan.SigningKey.ValueString()
		attrs.SigningKey = &v
	}
	if wsKnownBool(plan.LegacySignatureEnabled) {
		v := plan.LegacySignatureEnabled.ValueBool()
		attrs.LegacySignatureEnabled = &v
	}

	return api.CreateWebhookSubscriptionRequestDocument{
		Data: api.DataInCreateWebhookSubscriptionRequest{
			Type:       api.ResourceTypeWebhookSubscriptions,
			Attributes: attrs,
		},
	}, diags
}

// buildUpdateWebhookSubscriptionDocument builds the PATCH document from the plan.
//
//   - url and eventTypes are always sent; payloadMode, enabled and
//     legacySignatureEnabled whenever known (Optional+Computed: removing them
//     from the configuration keeps the current value).
//   - signingKey is sent only when the plan holds a value different from state
//     (including after import, where state has none). A null plan value is not
//     sent: the key cannot be cleared, and re-sending an unchanged key would
//     needlessly end a rotation overlap.
//
// No writable attribute is nullable on the server, so the document never
// needs explicit nulls.
func buildUpdateWebhookSubscriptionDocument(ctx context.Context, id string, plan, state webhookSubscriptionResourceModel) (api.UpdateWebhookSubscriptionRequestDocument, diag.Diagnostics) {
	var diags diag.Diagnostics

	attrs := &api.AttributesInUpdateWebhookSubscriptionRequest{
		OpenapiDiscriminator: api.ResourceTypeWebhookSubscriptions,
	}

	if wsKnownString(plan.URL) {
		v := plan.URL.ValueString()
		attrs.Url = &v
	}

	if !plan.EventTypes.IsNull() && !plan.EventTypes.IsUnknown() {
		eventTypes, d := webhookSubscriptionEventTypesFromSet(ctx, plan.EventTypes)
		diags.Append(d...)
		attrs.EventTypes = &eventTypes
	}

	if wsKnownString(plan.PayloadMode) {
		v := api.PayloadMode(plan.PayloadMode.ValueString())
		attrs.PayloadMode = &v
	}

	if wsKnownBool(plan.Enabled) {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}

	if wsKnownString(plan.SigningKey) && !plan.SigningKey.Equal(state.SigningKey) {
		v := plan.SigningKey.ValueString()
		attrs.SigningKey = &v
	}

	if wsKnownBool(plan.LegacySignatureEnabled) {
		v := plan.LegacySignatureEnabled.ValueBool()
		attrs.LegacySignatureEnabled = &v
	}

	return api.UpdateWebhookSubscriptionRequestDocument{
		Data: api.DataInUpdateWebhookSubscriptionRequest{
			Id:         id,
			Type:       api.ResourceTypeWebhookSubscriptions,
			Attributes: attrs,
		},
	}, diags
}

// buildUpdateWebhookSubscriptionBody encodes the PATCH body.
func buildUpdateWebhookSubscriptionBody(ctx context.Context, id string, plan, state webhookSubscriptionResourceModel) (io.Reader, diag.Diagnostics) {
	doc, diags := buildUpdateWebhookSubscriptionDocument(ctx, id, plan, state)
	if diags.HasError() {
		return nil, diags
	}
	body, err := client.MarshalWithNullAttributes(doc, nil)
	if err != nil {
		diags.AddError("Error Encoding Webhook Subscription Update", err.Error())
		return nil, diags
	}
	return body, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenWebhookSubscription maps the API response onto the Terraform state
// model. signing_key and generated_signing_key are never returned as
// attributes; the model's existing values (the plan on create/update, prior
// state on read) are left untouched, except that unknown becomes null.
func flattenWebhookSubscription(ctx context.Context, data api.DataInWebhookSubscriptionResponse, model *webhookSubscriptionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	var attrs api.AttributesInWebhookSubscriptionResponse
	if data.Attributes != nil {
		attrs = *data.Attributes
	}

	model.URL = wsStringPtrValue(attrs.Url)

	eventTypes := []string{}
	if attrs.EventTypes != nil {
		// The server de-duplicates, but a set value must not hold duplicates,
		// so guard against them here rather than fail the whole read.
		for _, et := range *attrs.EventTypes {
			if !slices.Contains(eventTypes, string(et)) {
				eventTypes = append(eventTypes, string(et))
			}
		}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, eventTypes)
	diags.Append(d...)
	model.EventTypes = set

	if attrs.PayloadMode != nil {
		model.PayloadMode = types.StringValue(string(*attrs.PayloadMode))
	} else {
		model.PayloadMode = types.StringNull()
	}

	model.Enabled = wsBoolPtrValue(attrs.Enabled)
	model.LegacySignatureEnabled = wsBoolPtrValue(attrs.LegacySignatureEnabled)

	model.CreateDateTime = timePtrToString(attrs.CreateDateTime)
	model.UpdateDateTime = timePtrToString(attrs.UpdateDateTime)
	model.PreviousSigningKeyExpiryDateTime = timePtrToString(attrs.PreviousSigningKeyExpiryDateTime)
	model.AutoDisabledDateTime = timePtrToString(attrs.AutoDisabledDateTime)

	if attrs.ConsecutiveFailureCount != nil {
		model.ConsecutiveFailureCount = types.Int64Value(int64(*attrs.ConsecutiveFailureCount))
	} else {
		model.ConsecutiveFailureCount = types.Int64Value(0)
	}

	// Write-only: never set from the response, but state must not be unknown.
	if model.SigningKey.IsUnknown() {
		model.SigningKey = types.StringNull()
	}
	if model.GeneratedSigningKey.IsUnknown() {
		model.GeneratedSigningKey = types.StringNull()
	}

	return diags
}

// webhookSubscriptionMetaSigningKey returns meta.signingKey of a created
// subscription: the key the server generated because the request had none.
func webhookSubscriptionMetaSigningKey(meta *api.Meta) (string, bool) {
	if meta == nil {
		return "", false
	}
	v, ok := (*meta)["signingKey"]
	if !ok || v == nil {
		return "", false
	}
	s, ok := (*v).(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func wsStringPtrValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func wsBoolPtrValue(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

// ---------------------------------------------------------------------------
// Plan modifiers
// ---------------------------------------------------------------------------

// wsGeneratedSigningKeyPlanModifier plans generated_signing_key:
//
//   - signing_key set in the configuration: null (the server generates nothing).
//   - signing_key unknown in the configuration: unknown (resolved on apply).
//   - otherwise, on create: unknown (the server may generate a key); on update:
//     the prior state value (the generated key is still the current one as far
//     as Terraform knows, or null if there never was one).
type wsGeneratedSigningKeyPlanModifier struct{}

func (m wsGeneratedSigningKeyPlanModifier) Description(_ context.Context) string {
	return "null when signing_key is set; otherwise the key generated on create, kept from state."
}

func (m wsGeneratedSigningKeyPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m wsGeneratedSigningKeyPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Destroy plan: nothing to do.
	if req.Plan.Raw.IsNull() {
		return
	}

	var signingKey types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("signing_key"), &signingKey)...)
	if resp.Diagnostics.HasError() {
		return
	}

	switch {
	case signingKey.IsUnknown():
		resp.PlanValue = types.StringUnknown()
	case !signingKey.IsNull():
		resp.PlanValue = types.StringNull()
	case req.State.Raw.IsNull():
		resp.PlanValue = types.StringUnknown()
	default:
		resp.PlanValue = req.StateValue
	}
}

// ---------------------------------------------------------------------------
// Validators (terraform-plugin-framework-validators is not a dependency)
// ---------------------------------------------------------------------------

type wsStringOneOfValidator struct{ allowed []string }

// wsStringOneOf validates that a string is one of allowed.
func wsStringOneOf(allowed ...string) wsStringOneOfValidator {
	return wsStringOneOfValidator{allowed: allowed}
}

func (v wsStringOneOfValidator) Description(_ context.Context) string {
	return "value must be one of: " + strings.Join(v.allowed, ", ")
}

func (v wsStringOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v wsStringOneOfValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !wsKnownString(req.ConfigValue) {
		return
	}
	if !slices.Contains(v.allowed, req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not allowed: %s.", req.ConfigValue.ValueString(), v.Description(ctx)))
	}
}

type wsSetElementsOneOfValidator struct{ allowed []string }

// wsSetElementsOneOf validates that every element of a set of strings is one of allowed.
func wsSetElementsOneOf(allowed ...string) wsSetElementsOneOfValidator {
	return wsSetElementsOneOfValidator{allowed: allowed}
}

func (v wsSetElementsOneOfValidator) Description(_ context.Context) string {
	return "each element must be one of: " + strings.Join(v.allowed, ", ")
}

func (v wsSetElementsOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v wsSetElementsOneOfValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, el := range req.ConfigValue.Elements() {
		s, ok := el.(types.String)
		if !ok || !wsKnownString(s) {
			continue
		}
		if !slices.Contains(v.allowed, s.ValueString()) {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
				fmt.Sprintf("%q is not allowed: %s.", s.ValueString(), v.Description(ctx)))
		}
	}
}

type wsSetSizeAtLeastValidator struct{ min int }

// wsSetSizeAtLeast validates that a set has at least min elements.
func wsSetSizeAtLeast(min int) wsSetSizeAtLeastValidator { return wsSetSizeAtLeastValidator{min: min} }

func (v wsSetSizeAtLeastValidator) Description(_ context.Context) string {
	return fmt.Sprintf("set must contain at least %d element(s)", v.min)
}

func (v wsSetSizeAtLeastValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v wsSetSizeAtLeastValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) < v.min {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value", v.Description(ctx)+".")
	}
}

type wsStringMinBytesValidator struct{ min int }

// wsStringMinBytes validates that a string is at least min bytes of UTF-8.
// Go strings are UTF-8, so len() is exactly the server's
// Encoding.UTF8.GetByteCount.
func wsStringMinBytes(min int) wsStringMinBytesValidator { return wsStringMinBytesValidator{min: min} }

func (v wsStringMinBytesValidator) Description(_ context.Context) string {
	return fmt.Sprintf("string must be at least %d bytes of UTF-8", v.min)
}

func (v wsStringMinBytesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v wsStringMinBytesValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !wsKnownString(req.ConfigValue) {
		return
	}
	// Do not echo the value: it is a secret.
	if n := len(req.ConfigValue.ValueString()); n < v.min {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%s, got %d.", v.Description(ctx), n))
	}
}

type wsHTTPSURLValidator struct{ maxLength int }

// wsHTTPSURL validates that a string is an absolute https URL with a host, of
// at most maxLength characters. The server's egress policy (blocked hosts and
// addresses) can only be checked by the server.
func wsHTTPSURL(maxLength int) wsHTTPSURLValidator { return wsHTTPSURLValidator{maxLength: maxLength} }

func (v wsHTTPSURLValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be an absolute https URL of at most %d characters", v.maxLength)
}

func (v wsHTTPSURLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v wsHTTPSURLValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !wsKnownString(req.ConfigValue) {
		return
	}
	s := req.ConfigValue.ValueString()
	// The server counts .NET UTF-16 characters; runes are a close-enough
	// approximation for a client-side check (the server remains authoritative).
	if n := len([]rune(s)); n > v.maxLength {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%s, got %d characters.", v.Description(ctx), n))
		return
	}
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Hostname() == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not allowed: %s.", s, v.Description(ctx)))
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// WebhookSubscriptionResourceModelForTest is a type alias for webhookSubscriptionResourceModel.
type WebhookSubscriptionResourceModelForTest = webhookSubscriptionResourceModel

// FlattenWebhookSubscriptionForTest exposes flattenWebhookSubscription for use in unit tests.
func FlattenWebhookSubscriptionForTest(ctx context.Context, data api.DataInWebhookSubscriptionResponse, model *webhookSubscriptionResourceModel) diag.Diagnostics {
	return flattenWebhookSubscription(ctx, data, model)
}

// BuildCreateWebhookSubscriptionBodyForTest exposes buildCreateWebhookSubscriptionBody for use in unit tests.
func BuildCreateWebhookSubscriptionBodyForTest(ctx context.Context, plan webhookSubscriptionResourceModel) (api.CreateWebhookSubscriptionRequestDocument, diag.Diagnostics) {
	return buildCreateWebhookSubscriptionBody(ctx, plan)
}

// BuildUpdateWebhookSubscriptionBodyForTest exposes buildUpdateWebhookSubscriptionBody for use in unit tests.
func BuildUpdateWebhookSubscriptionBodyForTest(ctx context.Context, id string, plan, state webhookSubscriptionResourceModel) (io.Reader, diag.Diagnostics) {
	return buildUpdateWebhookSubscriptionBody(ctx, id, plan, state)
}

// WebhookSubscriptionMetaSigningKeyForTest exposes webhookSubscriptionMetaSigningKey for use in unit tests.
func WebhookSubscriptionMetaSigningKeyForTest(meta *api.Meta) (string, bool) {
	return webhookSubscriptionMetaSigningKey(meta)
}

// WebhookSubscriptionValidatorsForTest exposes the url and signing_key validators for use in unit tests.
func WebhookSubscriptionValidatorsForTest() (urlValidator, signingKeyValidator validator.String) {
	return wsHTTPSURL(webhookSubscriptionMaxURLLength), wsStringMinBytes(webhookSubscriptionMinSigningKeyBytes)
}

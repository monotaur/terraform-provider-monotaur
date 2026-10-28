package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
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
	_ resource.Resource                = &notificationChannelResource{}
	_ resource.ResourceWithConfigure   = &notificationChannelResource{}
	_ resource.ResourceWithImportState = &notificationChannelResource{}
)

// Server-side limits (Monotaur.Core NotificationChannel / NotificationChannelResourceService).
const (
	notificationChannelMaxNameLength       = 200
	notificationChannelMaxTemplateLength   = 10000
	notificationChannelMaxCredentialLength = 2048
)

// notificationChannelTypes are the values of api.NotificationChannelType.
var notificationChannelTypes = []string{
	string(api.Email),
	string(api.PagerDuty),
	string(api.Slack),
	string(api.Teams),
	string(api.Webhook),
}

// notificationChannelEventTypes are the CloudEvent types a channel may take:
// every api.WebhookEventType except the webhook-only acknowledged and
// note-added events, which the server rejects with 422 for channels. Legacy
// event names are not accepted here: the server normalises them to these
// CloudEvent names, which would surface as an inconsistent result after apply.
var notificationChannelEventTypes = []string{
	string(api.ComMonotaurAlarmCleared),
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

// NewNotificationChannelResource returns a new notificationChannelResource constructor function.
func NewNotificationChannelResource() resource.Resource {
	return &notificationChannelResource{}
}

// notificationChannelResource implements the monotaur_notification_channel managed resource.
type notificationChannelResource struct {
	client *client.Client
}

// notificationChannelResourceModel is the Terraform state model for a notification channel.
//
// Attribute mapping:
//
//	AttributesInCreateNotificationChannelRequest: name, type, eventTypes (required); enabled,
//	                                              credential, recipients, subjectTemplate,
//	                                              bodyTemplate (optional)
//	AttributesInUpdateNotificationChannelRequest: as create, minus type (create-only)
//	AttributesInNotificationChannelResponse:      all of the above except credential (write-only,
//	                                              never returned), plus createDateTime,
//	                                              updateDateTime, consecutiveFailureCount,
//	                                              autoDisabledDateTime (computed)
type notificationChannelResourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Type                    types.String `tfsdk:"type"`
	EventTypes              types.Set    `tfsdk:"event_types"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
	Credential              types.String `tfsdk:"credential"`
	Recipients              types.List   `tfsdk:"recipients"`
	SubjectTemplate         types.String `tfsdk:"subject_template"`
	BodyTemplate            types.String `tfsdk:"body_template"`
	CreateDateTime          types.String `tfsdk:"create_date_time"`
	UpdateDateTime          types.String `tfsdk:"update_date_time"`
	ConsecutiveFailureCount types.Int64  `tfsdk:"consecutive_failure_count"`
	AutoDisabledDateTime    types.String `tfsdk:"auto_disabled_date_time"`
}

// Metadata sets the resource type name.
func (r *notificationChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_channel"
}

// Schema defines the Terraform schema for the resource.
func (r *notificationChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur notification channel. A channel sends the events it subscribes to " +
			"straight to Slack, Microsoft Teams, email, PagerDuty or any HTTPS endpoint, rendered with Scriban templates.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the notification channel (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the channel (1 to 200 characters), also `channel.name` in templates.",
				Required:            true,
				Validators: []validator.String{
					ncStringLengthBetween(1, notificationChannelMaxNameLength),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "What the channel delivers to: one of `Slack`, `Teams`, `Email`, `PagerDuty`, `Webhook`. " +
					"Cannot be changed after creation; changing it destroys and recreates the channel.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					ncStringOneOf(notificationChannelTypes...),
				},
			},
			"event_types": schema.SetAttribute{
				MarkdownDescription: "The CloudEvent types delivered to the channel (at least one), e.g. " +
					"`com.monotaur.alarm.triggered`. `com.monotaur.alarm.acknowledged` and " +
					"`com.monotaur.alarm.note-added` are webhook-subscription only and not accepted.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					ncSetSizeAtLeast(1),
					ncSetElementsOneOf(notificationChannelEventTypes...),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the channel sends notifications. Defaults to `true` on the server. " +
					"Delivery sets it to `false` after too many failed deliveries in a row; setting it to `true` " +
					"again resets `consecutive_failure_count` and `auto_disabled_date_time`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"credential": schema.StringAttribute{
				MarkdownDescription: "The Slack, Teams or webhook URL (must be `https`), or the PagerDuty Events API v2 " +
					"integration key. Required for those types and not allowed for `Email`. Sensitive and write-only: " +
					"the API never returns it, so the value from the configuration is kept in state, drift is not " +
					"detected, and `terraform import` cannot recover it (the next apply re-sends the configured value). " +
					"Removing it from the configuration does not clear it on the server.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					ncStringLengthBetween(1, notificationChannelMaxCredentialLength),
				},
			},
			"recipients": schema.ListAttribute{
				MarkdownDescription: "Email addresses (`ops@example.com` or `Ops <ops@example.com>`), 1 to 20. " +
					"Required for `Email` channels and not allowed for the other types.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"subject_template": schema.StringAttribute{
				MarkdownDescription: "Scriban template of the email subject, the PagerDuty alert summary or the Teams card " +
					"title (at most 10,000 characters). Not allowed for `Slack` and `Webhook`. When unset, the type's " +
					"default template is used.",
				Optional: true,
				Validators: []validator.String{
					ncStringLengthBetween(1, notificationChannelMaxTemplateLength),
				},
			},
			"body_template": schema.StringAttribute{
				MarkdownDescription: "Scriban template of the message body (at most 10,000 characters); for `Webhook` it " +
					"must render JSON. When unset, the type's default template is used.",
				Optional: true,
				Validators: []validator.String{
					ncStringLengthBetween(1, notificationChannelMaxTemplateLength),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the channel was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the channel was last updated (assigned by the API).",
				Computed:            true,
			},
			"consecutive_failure_count": schema.Int64Attribute{
				MarkdownDescription: "Deliveries in a row that ended `Failed` (system-managed, read-only).",
				Computed:            true,
			},
			"auto_disabled_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when delivery disabled the channel after too many failures, " +
					"or null (system-managed, read-only).",
				Computed: true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *notificationChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new notification channel via POST /notificationChannels.
func (r *notificationChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan notificationChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildCreateNotificationChannelBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_channel: creating notification channel", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostNotificationChannelWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostNotificationChannelParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Channel", "Could not create notification channel: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Channel", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationChannelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationChannel(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_channel: created notification channel", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *notificationChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state notificationChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_channel: reading notification channel", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetNotificationChannel(ctx, id, &api.GetNotificationChannelParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel", "Could not read notification channel "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_channel: notification channel not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Notification Channel", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationChannelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel Response", err.Error())
		return
	}

	// Pass the existing state so flattenNotificationChannel keeps the
	// write-only credential, which the API never returns.
	resp.Diagnostics.Append(flattenNotificationChannel(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing notification channel via PATCH /notificationChannels/{id}.
func (r *notificationChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan notificationChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state notificationChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	reqBody, diags := buildUpdateNotificationChannelBody(ctx, id, plan, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_channel: updating notification channel", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchNotificationChannelWithBody(ctx, id, &api.PatchNotificationChannelParams{}, client.ContentType, reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Channel", "Could not update notification channel "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Channel", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetNotificationChannel(ctx, id, &api.GetNotificationChannelParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInNotificationChannelResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel Response", err.Error())
		return
	}

	// Pass plan so flattenNotificationChannel keeps the planned credential.
	resp.Diagnostics.Append(flattenNotificationChannel(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_channel: updated notification channel", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a notification channel via DELETE /notificationChannels/{id}.
func (r *notificationChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notificationChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_channel: deleting notification channel", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteNotificationChannel(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Notification Channel", "Could not delete notification channel "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_channel: notification channel already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Notification Channel", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_notification_channel: deleted notification channel", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_notification_channel.example <id>`.
// The credential cannot be imported: the API never returns it.
func (r *notificationChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request builders
// ---------------------------------------------------------------------------

// notificationChannelEventTypesFromSet converts the event_types set into the API enum slice.
func notificationChannelEventTypesFromSet(ctx context.Context, set types.Set) ([]api.WebhookEventType, diag.Diagnostics) {
	var values []string
	diags := set.ElementsAs(ctx, &values, false)
	out := make([]api.WebhookEventType, len(values))
	for i, v := range values {
		out[i] = api.WebhookEventType(v)
	}
	return out, diags
}

// ncKnownString reports whether v holds a concrete (non-null, non-unknown) value.
func ncKnownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

// buildCreateNotificationChannelBody builds the POST body from the plan.
func buildCreateNotificationChannelBody(ctx context.Context, plan notificationChannelResourceModel) (api.CreateNotificationChannelRequestDocument, diag.Diagnostics) {
	var diags diag.Diagnostics

	channelType := api.NotificationChannelType(plan.Type.ValueString())
	attrs := &api.AttributesInCreateNotificationChannelRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationChannels,
		Name:                 plan.Name.ValueString(),
		Type:                 &channelType,
		// Recipients has no omitempty in the generated struct; send [] rather than null.
		Recipients: []string{},
	}

	eventTypes, d := notificationChannelEventTypesFromSet(ctx, plan.EventTypes)
	diags.Append(d...)
	attrs.EventTypes = eventTypes

	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}
	if ncKnownString(plan.Credential) {
		v := plan.Credential.ValueString()
		attrs.Credential = &v
	}
	if !plan.Recipients.IsNull() && !plan.Recipients.IsUnknown() {
		recipients, d := stringListToSlice(ctx, plan.Recipients)
		diags.Append(d...)
		if recipients != nil {
			attrs.Recipients = recipients
		}
	}
	if ncKnownString(plan.SubjectTemplate) {
		v := plan.SubjectTemplate.ValueString()
		attrs.SubjectTemplate = &v
	}
	if ncKnownString(plan.BodyTemplate) {
		v := plan.BodyTemplate.ValueString()
		attrs.BodyTemplate = &v
	}

	return api.CreateNotificationChannelRequestDocument{
		Data: api.DataInCreateNotificationChannelRequest{
			Type:       api.ResourceTypeNotificationChannels,
			Attributes: attrs,
		},
	}, diags
}

// buildUpdateNotificationChannelDocument builds the PATCH document from the
// plan and returns the JSON attribute names that must be sent as explicit
// nulls (nullable Optional attributes whose plan value is null).
//
//   - type is create-only (RequiresReplace) and never sent.
//   - credential is sent only when the plan holds a value different from state
//     (including after import, where state has none). A null plan value is not
//     sent: the server rejects an empty credential for types that need one,
//     and an Email channel has none to clear.
//   - subjectTemplate/bodyTemplate are sent as null when unset, so removing
//     them from the configuration restores the type's default template.
func buildUpdateNotificationChannelDocument(ctx context.Context, id string, plan, state notificationChannelResourceModel) (api.UpdateNotificationChannelRequestDocument, []string, diag.Diagnostics) {
	var diags diag.Diagnostics
	var nullAttrs []string

	name := plan.Name.ValueString()
	attrs := &api.AttributesInUpdateNotificationChannelRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationChannels,
		Name:                 &name,
	}

	if !plan.EventTypes.IsNull() && !plan.EventTypes.IsUnknown() {
		eventTypes, d := notificationChannelEventTypesFromSet(ctx, plan.EventTypes)
		diags.Append(d...)
		attrs.EventTypes = &eventTypes
	}

	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}

	if ncKnownString(plan.Credential) && !plan.Credential.Equal(state.Credential) {
		v := plan.Credential.ValueString()
		attrs.Credential = &v
	}

	if !plan.Recipients.IsNull() && !plan.Recipients.IsUnknown() {
		recipients, d := stringListToSlice(ctx, plan.Recipients)
		diags.Append(d...)
		if recipients == nil {
			recipients = []string{}
		}
		attrs.Recipients = &recipients
	}

	switch {
	case ncKnownString(plan.SubjectTemplate):
		v := plan.SubjectTemplate.ValueString()
		attrs.SubjectTemplate = &v
	case plan.SubjectTemplate.IsNull():
		nullAttrs = append(nullAttrs, "subjectTemplate")
	}

	switch {
	case ncKnownString(plan.BodyTemplate):
		v := plan.BodyTemplate.ValueString()
		attrs.BodyTemplate = &v
	case plan.BodyTemplate.IsNull():
		nullAttrs = append(nullAttrs, "bodyTemplate")
	}

	return api.UpdateNotificationChannelRequestDocument{
		Data: api.DataInUpdateNotificationChannelRequest{
			Id:         id,
			Type:       api.ResourceTypeNotificationChannels,
			Attributes: attrs,
		},
	}, nullAttrs, diags
}

// buildUpdateNotificationChannelBody encodes the PATCH body, including explicit nulls.
func buildUpdateNotificationChannelBody(ctx context.Context, id string, plan, state notificationChannelResourceModel) (io.Reader, diag.Diagnostics) {
	doc, nullAttrs, diags := buildUpdateNotificationChannelDocument(ctx, id, plan, state)
	if diags.HasError() {
		return nil, diags
	}
	body, err := client.MarshalWithNullAttributes(doc, nullAttrs)
	if err != nil {
		diags.AddError("Error Encoding Notification Channel Update", err.Error())
		return nil, diags
	}
	return body, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenNotificationChannel maps the API response onto the Terraform state model.
// The credential is never returned by the API; the model's existing value (the
// plan on create/update, prior state on read) is left untouched.
func flattenNotificationChannel(ctx context.Context, data api.DataInNotificationChannelResponse, model *notificationChannelResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	var attrs api.AttributesInNotificationChannelResponse
	if data.Attributes != nil {
		attrs = *data.Attributes
	}

	model.Name = ncStringPtrValue(attrs.Name)

	if attrs.Type != nil {
		model.Type = types.StringValue(string(*attrs.Type))
	} else {
		model.Type = types.StringNull()
	}

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

	if attrs.Enabled != nil {
		model.Enabled = types.BoolValue(*attrs.Enabled)
	} else {
		model.Enabled = types.BoolNull()
	}

	recipients := []string{}
	if attrs.Recipients != nil {
		recipients = append(recipients, *attrs.Recipients...)
	}
	list, d := types.ListValueFrom(ctx, types.StringType, recipients)
	diags.Append(d...)
	model.Recipients = list

	model.SubjectTemplate = ncStringPtrValue(attrs.SubjectTemplate)
	model.BodyTemplate = ncStringPtrValue(attrs.BodyTemplate)
	model.CreateDateTime = timePtrToString(attrs.CreateDateTime)
	model.UpdateDateTime = timePtrToString(attrs.UpdateDateTime)
	model.AutoDisabledDateTime = timePtrToString(attrs.AutoDisabledDateTime)

	if attrs.ConsecutiveFailureCount != nil {
		model.ConsecutiveFailureCount = types.Int64Value(int64(*attrs.ConsecutiveFailureCount))
	} else {
		model.ConsecutiveFailureCount = types.Int64Value(0)
	}

	// credential: write-only, intentionally not set here. An unknown value
	// cannot occur (the attribute is not Computed), but guard anyway so state
	// is never left unknown.
	if model.Credential.IsUnknown() {
		model.Credential = types.StringNull()
	}

	return diags
}

func ncStringPtrValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// ---------------------------------------------------------------------------
// Validators (terraform-plugin-framework-validators is not a dependency)
// ---------------------------------------------------------------------------

type ncStringOneOfValidator struct{ allowed []string }

// ncStringOneOf validates that a string is one of allowed.
func ncStringOneOf(allowed ...string) ncStringOneOfValidator {
	return ncStringOneOfValidator{allowed: allowed}
}

func (v ncStringOneOfValidator) Description(_ context.Context) string {
	return "value must be one of: " + strings.Join(v.allowed, ", ")
}

func (v ncStringOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ncStringOneOfValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	if !slices.Contains(v.allowed, req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not allowed: %s.", req.ConfigValue.ValueString(), v.Description(ctx)))
	}
}

type ncSetElementsOneOfValidator struct{ allowed []string }

// ncSetElementsOneOf validates that every element of a set of strings is one of allowed.
func ncSetElementsOneOf(allowed ...string) ncSetElementsOneOfValidator {
	return ncSetElementsOneOfValidator{allowed: allowed}
}

func (v ncSetElementsOneOfValidator) Description(_ context.Context) string {
	return "each element must be one of: " + strings.Join(v.allowed, ", ")
}

func (v ncSetElementsOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ncSetElementsOneOfValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, el := range req.ConfigValue.Elements() {
		s, ok := el.(types.String)
		if !ok || !ncKnownString(s) {
			continue
		}
		if !slices.Contains(v.allowed, s.ValueString()) {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
				fmt.Sprintf("%q is not allowed: %s.", s.ValueString(), v.Description(ctx)))
		}
	}
}

type ncSetSizeAtLeastValidator struct{ min int }

// ncSetSizeAtLeast validates that a set has at least min elements.
func ncSetSizeAtLeast(min int) ncSetSizeAtLeastValidator { return ncSetSizeAtLeastValidator{min: min} }

func (v ncSetSizeAtLeastValidator) Description(_ context.Context) string {
	return fmt.Sprintf("set must contain at least %d element(s)", v.min)
}

func (v ncSetSizeAtLeastValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ncSetSizeAtLeastValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) < v.min {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value", v.Description(ctx)+".")
	}
}

type ncStringLengthBetweenValidator struct{ min, max int }

// ncStringLengthBetween validates that a string has between min and max characters.
func ncStringLengthBetween(min, max int) ncStringLengthBetweenValidator {
	return ncStringLengthBetweenValidator{min: min, max: max}
}

func (v ncStringLengthBetweenValidator) Description(_ context.Context) string {
	return fmt.Sprintf("string length must be between %d and %d", v.min, v.max)
}

func (v ncStringLengthBetweenValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ncStringLengthBetweenValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	// The server counts .NET UTF-16 characters; runes are a close-enough
	// approximation for a client-side check (the server remains authoritative).
	n := len([]rune(req.ConfigValue.ValueString()))
	if n < v.min || n > v.max {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%s, got %d.", v.Description(ctx), n))
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// NotificationChannelResourceModelForTest is a type alias for notificationChannelResourceModel.
type NotificationChannelResourceModelForTest = notificationChannelResourceModel

// FlattenNotificationChannelForTest exposes flattenNotificationChannel for use in unit tests.
func FlattenNotificationChannelForTest(ctx context.Context, data api.DataInNotificationChannelResponse, model *notificationChannelResourceModel) diag.Diagnostics {
	return flattenNotificationChannel(ctx, data, model)
}

// BuildCreateNotificationChannelBodyForTest exposes buildCreateNotificationChannelBody for use in unit tests.
func BuildCreateNotificationChannelBodyForTest(ctx context.Context, plan notificationChannelResourceModel) (api.CreateNotificationChannelRequestDocument, diag.Diagnostics) {
	return buildCreateNotificationChannelBody(ctx, plan)
}

// BuildUpdateNotificationChannelBodyForTest exposes buildUpdateNotificationChannelBody for use in unit tests.
func BuildUpdateNotificationChannelBodyForTest(ctx context.Context, id string, plan, state notificationChannelResourceModel) (io.Reader, diag.Diagnostics) {
	return buildUpdateNotificationChannelBody(ctx, id, plan, state)
}

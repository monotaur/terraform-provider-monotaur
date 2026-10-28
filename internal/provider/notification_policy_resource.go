package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &notificationPolicyResource{}
	_ resource.ResourceWithConfigure   = &notificationPolicyResource{}
	_ resource.ResourceWithImportState = &notificationPolicyResource{}
)

// NewNotificationPolicyResource returns a new notificationPolicyResource constructor function.
func NewNotificationPolicyResource() resource.Resource {
	return &notificationPolicyResource{}
}

// notificationPolicyResource implements the monotaur_notification_policy managed resource.
type notificationPolicyResource struct {
	client *client.Client
}

// notificationPolicyResourceModel is the Terraform state model for a notification policy.
//
// Attribute mapping:
//
//	AttributesInCreateNotificationPolicyRequest: name (required), enabled, minimumSeverity,
//	                                             repeatIntervalMinutes (optional)
//	AttributesInNotificationPolicyResponse:      id (computed), name, enabled, minimumSeverity,
//	                                             repeatIntervalMinutes, createDateTime (computed),
//	                                             updateDateTime (computed)
//	RelationshipsIn*NotificationPolicyRequest:   channel_ids, label_ids, component_ids,
//	                                             monitor_ids (to-many, optional)
//
// The read-only `escalationSteps` relationship is deliberately not part of the
// schema: steps are managed by monotaur_notification_escalation_step through
// its notification_policy_id, and surfacing them here would make the policy
// show a diff every time a step is added or removed.
type notificationPolicyResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Enabled               types.Bool   `tfsdk:"enabled"`
	MinimumSeverity       types.String `tfsdk:"minimum_severity"`
	RepeatIntervalMinutes types.Int64  `tfsdk:"repeat_interval_minutes"`
	CreateDateTime        types.String `tfsdk:"create_date_time"`
	UpdateDateTime        types.String `tfsdk:"update_date_time"`
	ChannelIDs            types.List   `tfsdk:"channel_ids"`
	LabelIDs              types.List   `tfsdk:"label_ids"`
	ComponentIDs          types.List   `tfsdk:"component_ids"`
	MonitorIDs            types.List   `tfsdk:"monitor_ids"`
}

// Metadata sets the resource type name.
func (r *notificationPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_policy"
}

// Schema defines the Terraform schema for the resource.
func (r *notificationPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur notification policy. A policy routes events to notification channels " +
			"by label, component, monitor and alarm severity, and can repeat an unacknowledged alarm at an interval. " +
			"Once any policy names a channel, that channel only receives events a policy targeting it matches. " +
			"Escalation steps are managed separately with `monotaur_notification_escalation_step`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the notification policy (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the policy (1 to 200 characters).",
				Required:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy matches events. A disabled policy matches nothing but its channels stay routed. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"minimum_severity": schema.StringAttribute{
				MarkdownDescription: "The least severe alarm the policy matches: `Info`, `Warning`, `Error` or `Critical`. " +
					"Omit to match any severity. Only alarm events have a severity, so setting this excludes every other event.",
				Optional: true,
			},
			"repeat_interval_minutes": schema.Int64Attribute{
				MarkdownDescription: "How often, in minutes (1 to 10080), an open and unacknowledged alarm is notified again to the policy's channels. Omit to notify once.",
				Optional:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the policy was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the policy was last updated (assigned by the API).",
				Computed:            true,
			},
			"channel_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the notification channels notified of matching events and of repeats. Set to `[]` to remove all.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels to match: the event's monitor or one of its components carries one of them. Set to `[]` to remove all.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components to match: one of them is among the event's components. Set to `[]` to remove all.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of monitors to match: the event's monitor is one of them. Set to `[]` to remove all.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *notificationPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new notification policy via POST /notificationPolicies.
func (r *notificationPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan notificationPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := buildCreateNotificationPolicyAttributes(plan)

	rels, diags := buildCreateNotificationPolicyRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateNotificationPolicyRequestDocument{
		Data: api.DataInCreateNotificationPolicyRequest{
			Type:          api.ResourceTypeNotificationPolicies,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_notification_policy: creating notification policy", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostNotificationPolicyWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostNotificationPolicyParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Policy", "Could not create notification policy: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Policy", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationPolicyResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationPolicy(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_policy: created notification policy", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *notificationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state notificationPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_policy: reading notification policy", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetNotificationPolicy(ctx, id, notificationPolicyReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy", "Could not read notification policy "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the policy was deleted out-of-band — remove it from state so
		// the next plan re-creates it instead of erroring on a missing resource.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_policy: notification policy not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Notification Policy", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationPolicyResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationPolicy(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing notification policy via PATCH /notificationPolicies/{id}.
func (r *notificationPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan notificationPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state notificationPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	rels, diags := buildUpdateNotificationPolicyRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateNotificationPolicyRequestDocument{
		Data: api.DataInUpdateNotificationPolicyRequest{
			Id:            id,
			Type:          api.ResourceTypeNotificationPolicies,
			Attributes:    buildUpdateNotificationPolicyAttributes(plan),
			Relationships: rels,
		},
	}

	// The generated attribute struct omits nil pointers, which JSON:API reads
	// as "no change". Send explicit nulls for nullable attributes the
	// configuration no longer sets so that removing them clears them.
	reader, err := client.MarshalWithNullAttributes(body, notificationPolicyNullAttributes(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Policy", "Could not encode request: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_notification_policy: updating notification policy", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchNotificationPolicyWithBody(ctx, id, &api.PatchNotificationPolicyParams{}, client.ContentType, reader)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Policy", "Could not update notification policy "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Policy", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetNotificationPolicy(ctx, id, notificationPolicyReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInNotificationPolicyResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationPolicy(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_policy: updated notification policy", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a notification policy via DELETE /notificationPolicies/{id}.
// The API cascades the delete to the policy's escalation steps.
func (r *notificationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notificationPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_policy: deleting notification policy", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteNotificationPolicy(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Notification Policy", "Could not delete notification policy "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_policy: notification policy already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Notification Policy", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_notification_policy: deleted notification policy", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_notification_policy.example <id>`.
func (r *notificationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// notificationPolicyReadParams requests the writable to-many relationships via
// `?include=` so the response carries their `data` (a bare GET echoes `links`
// only, which would leave imported policies without IDs and hide drift).
// Included to-many collections are paginated by the API (default 10), so each
// one is scoped to the maximum page size.
func notificationPolicyReadParams() *api.GetNotificationPolicyParams {
	include := "channels,labels,components,monitors"
	pageSize := "channels:500,labels:500,components:500,monitors:500"
	query := map[string]*string{"include": &include, "page[size]": &pageSize}
	return &api.GetNotificationPolicyParams{Query: &query}
}

// buildCreateNotificationPolicyAttributes maps the plan onto the create attributes.
func buildCreateNotificationPolicyAttributes(plan notificationPolicyResourceModel) *api.AttributesInCreateNotificationPolicyRequest {
	attrs := &api.AttributesInCreateNotificationPolicyRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationPolicies,
		Name:                 plan.Name.ValueString(),
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}
	if !plan.MinimumSeverity.IsNull() && !plan.MinimumSeverity.IsUnknown() {
		v := api.AlarmSeverity(plan.MinimumSeverity.ValueString())
		attrs.MinimumSeverity = &v
	}
	if !plan.RepeatIntervalMinutes.IsNull() && !plan.RepeatIntervalMinutes.IsUnknown() {
		v := int32(plan.RepeatIntervalMinutes.ValueInt64())
		attrs.RepeatIntervalMinutes = &v
	}
	return attrs
}

// buildUpdateNotificationPolicyAttributes maps the plan onto the update
// attributes. Null nullable attributes are left nil here; see
// notificationPolicyNullAttributes for how they are cleared.
func buildUpdateNotificationPolicyAttributes(plan notificationPolicyResourceModel) *api.AttributesInUpdateNotificationPolicyRequest {
	name := plan.Name.ValueString()
	attrs := &api.AttributesInUpdateNotificationPolicyRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationPolicies,
		Name:                 &name,
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		attrs.Enabled = &v
	}
	if !plan.MinimumSeverity.IsNull() && !plan.MinimumSeverity.IsUnknown() {
		v := api.AlarmSeverity(plan.MinimumSeverity.ValueString())
		attrs.MinimumSeverity = &v
	}
	if !plan.RepeatIntervalMinutes.IsNull() && !plan.RepeatIntervalMinutes.IsUnknown() {
		v := int32(plan.RepeatIntervalMinutes.ValueInt64())
		attrs.RepeatIntervalMinutes = &v
	}
	return attrs
}

// notificationPolicyNullAttributes returns the JSON names of the nullable,
// Optional (non-Computed) attributes whose plan value is null. They are sent
// as explicit JSON nulls on update so that removing them from configuration
// clears them on the server.
func notificationPolicyNullAttributes(plan notificationPolicyResourceModel) []string {
	var names []string
	if plan.MinimumSeverity.IsNull() {
		names = append(names, "minimumSeverity")
	}
	if plan.RepeatIntervalMinutes.IsNull() {
		names = append(names, "repeatIntervalMinutes")
	}
	return names
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// policyRelKnownIDs returns the elements of a known, non-null list. ok is false when
// the list is null or unknown, meaning "leave the relationship unchanged". An
// empty known list returns a non-nil empty slice so it serializes as
// `"data": []` and clears the relationship.
func policyRelKnownIDs(ctx context.Context, list types.List) (ids []string, ok bool, diags diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, false, nil
	}
	ids, diags = stringListToSlice(ctx, list)
	if diags.HasError() {
		return nil, false, diags
	}
	return ids, true, diags
}

func policyRelChannelIdentifiers(ids []string) *api.ToManyNotificationChannelInRequest {
	data := make([]api.NotificationChannelIdentifierInRequest, len(ids))
	for i, id := range ids {
		data[i] = api.NotificationChannelIdentifierInRequest{Id: id, Type: api.ResourceTypeNotificationChannels}
	}
	return &api.ToManyNotificationChannelInRequest{Data: data}
}

// notificationPolicyRelationships holds the to-many request payloads shared by
// the create and update builders.
type notificationPolicyRelationships struct {
	channels   *api.ToManyNotificationChannelInRequest
	labels     *api.ToManyLabelInRequest
	components *api.ToManyComponentInRequest
	monitors   *api.ToManyMonitorInRequest
}

func buildNotificationPolicyRelationships(ctx context.Context, plan notificationPolicyResourceModel) (notificationPolicyRelationships, diag.Diagnostics) {
	var out notificationPolicyRelationships
	var diags diag.Diagnostics

	if ids, ok, d := policyRelKnownIDs(ctx, plan.ChannelIDs); d.HasError() {
		diags.Append(d...)
	} else if ok {
		out.channels = policyRelChannelIdentifiers(ids)
	}

	if ids, ok, d := policyRelKnownIDs(ctx, plan.LabelIDs); d.HasError() {
		diags.Append(d...)
	} else if ok {
		data := make([]api.LabelIdentifierInRequest, len(ids))
		for i, id := range ids {
			data[i] = api.LabelIdentifierInRequest{Id: id, Type: api.ResourceTypeLabels}
		}
		out.labels = &api.ToManyLabelInRequest{Data: data}
	}

	if ids, ok, d := policyRelKnownIDs(ctx, plan.ComponentIDs); d.HasError() {
		diags.Append(d...)
	} else if ok {
		data := make([]api.ComponentIdentifierInRequest, len(ids))
		for i, id := range ids {
			data[i] = api.ComponentIdentifierInRequest{Id: id, Type: api.ResourceTypeComponents}
		}
		out.components = &api.ToManyComponentInRequest{Data: data}
	}

	if ids, ok, d := policyRelKnownIDs(ctx, plan.MonitorIDs); d.HasError() {
		diags.Append(d...)
	} else if ok {
		data := make([]api.MonitorIdentifierInRequest, len(ids))
		for i, id := range ids {
			data[i] = api.MonitorIdentifierInRequest{Id: id, Type: api.ResourceTypeMonitors}
		}
		out.monitors = &api.ToManyMonitorInRequest{Data: data}
	}

	return out, diags
}

// buildCreateNotificationPolicyRelationships converts the plan's relationship
// ID fields into the API request type for create operations. escalationSteps
// is read-only and never sent.
func buildCreateNotificationPolicyRelationships(ctx context.Context, plan notificationPolicyResourceModel) (*api.RelationshipsInCreateNotificationPolicyRequest, diag.Diagnostics) {
	r, diags := buildNotificationPolicyRelationships(ctx, plan)
	return &api.RelationshipsInCreateNotificationPolicyRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationPolicies,
		Channels:             r.channels,
		Labels:               r.labels,
		Components:           r.components,
		Monitors:             r.monitors,
	}, diags
}

// buildUpdateNotificationPolicyRelationships converts the plan's relationship
// ID fields into the API request type for update operations. An empty list is
// sent as `"data": []`, which clears the relationship.
func buildUpdateNotificationPolicyRelationships(ctx context.Context, plan notificationPolicyResourceModel) (*api.RelationshipsInUpdateNotificationPolicyRequest, diag.Diagnostics) {
	r, diags := buildNotificationPolicyRelationships(ctx, plan)
	return &api.RelationshipsInUpdateNotificationPolicyRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationPolicies,
		Channels:             r.channels,
		Labels:               r.labels,
		Components:           r.components,
		Monitors:             r.monitors,
	}, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// policyRelFlattenIDList maps a to-many relationship onto a list attribute.
//
//   - present=false (the API echoed `links` only): keep the prior value, the
//     plan for create/update or the state for read, as monitor_resource.go
//     does; a null/unknown prior becomes an empty list.
//   - present=true: use the API's IDs, but keep the prior list when it holds
//     the same IDs in a different order, so an ordering difference between
//     configuration and API never shows up as a diff.
func policyRelFlattenIDList(ctx context.Context, prior types.List, ids []string, present bool) (types.List, diag.Diagnostics) {
	if !present {
		if prior.IsNull() || prior.IsUnknown() {
			return types.ListValueMust(types.StringType, []attr.Value{}), nil
		}
		return prior, nil
	}

	if !prior.IsNull() && !prior.IsUnknown() {
		priorIDs, d := stringListToSlice(ctx, prior)
		if !d.HasError() && policyRelSameIDSet(priorIDs, ids) {
			return prior, nil
		}
	}

	if ids == nil {
		ids = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, ids)
}

// policyRelSameIDSet reports whether a and b contain the same IDs with the same
// multiplicities, ignoring order.
func policyRelSameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, id := range a {
		counts[id]++
	}
	for _, id := range b {
		counts[id]--
		if counts[id] < 0 {
			return false
		}
	}
	return true
}

// flattenNotificationPolicy maps the API DataInNotificationPolicyResponse onto the Terraform state model.
func flattenNotificationPolicy(ctx context.Context, data api.DataInNotificationPolicyResponse, model *notificationPolicyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Enabled != nil {
			model.Enabled = types.BoolValue(*attrs.Enabled)
		} else {
			model.Enabled = types.BoolNull()
		}

		if attrs.MinimumSeverity != nil {
			model.MinimumSeverity = types.StringValue(string(*attrs.MinimumSeverity))
		} else {
			model.MinimumSeverity = types.StringNull()
		}

		if attrs.RepeatIntervalMinutes != nil {
			model.RepeatIntervalMinutes = types.Int64Value(int64(*attrs.RepeatIntervalMinutes))
		} else {
			model.RepeatIntervalMinutes = types.Int64Null()
		}

		if attrs.CreateDateTime != nil {
			model.CreateDateTime = types.StringValue(attrs.CreateDateTime.Format(time.RFC3339))
		} else {
			model.CreateDateTime = types.StringNull()
		}

		if attrs.UpdateDateTime != nil {
			model.UpdateDateTime = types.StringValue(attrs.UpdateDateTime.Format(time.RFC3339))
		} else {
			model.UpdateDateTime = types.StringNull()
		}
	}

	rels := data.Relationships
	if rels == nil {
		rels = &api.RelationshipsInNotificationPolicyResponse{}
	}

	var (
		ids     []string
		present bool
		list    types.List
		d       diag.Diagnostics
	)

	ids, present = nil, rels.Channels != nil && rels.Channels.Data != nil
	if present {
		for _, item := range *rels.Channels.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d = policyRelFlattenIDList(ctx, model.ChannelIDs, ids, present)
	diags.Append(d...)
	model.ChannelIDs = list

	ids, present = nil, rels.Labels != nil && rels.Labels.Data != nil
	if present {
		for _, item := range *rels.Labels.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d = policyRelFlattenIDList(ctx, model.LabelIDs, ids, present)
	diags.Append(d...)
	model.LabelIDs = list

	ids, present = nil, rels.Components != nil && rels.Components.Data != nil
	if present {
		for _, item := range *rels.Components.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d = policyRelFlattenIDList(ctx, model.ComponentIDs, ids, present)
	diags.Append(d...)
	model.ComponentIDs = list

	ids, present = nil, rels.Monitors != nil && rels.Monitors.Data != nil
	if present {
		for _, item := range *rels.Monitors.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d = policyRelFlattenIDList(ctx, model.MonitorIDs, ids, present)
	diags.Append(d...)
	model.MonitorIDs = list

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// NotificationPolicyResourceModelForTest is a type alias for
// notificationPolicyResourceModel for use in unit tests.
type NotificationPolicyResourceModelForTest = notificationPolicyResourceModel

// FlattenNotificationPolicyForTest exposes flattenNotificationPolicy for use in unit tests.
func FlattenNotificationPolicyForTest(ctx context.Context, data api.DataInNotificationPolicyResponse, model *notificationPolicyResourceModel) diag.Diagnostics {
	return flattenNotificationPolicy(ctx, data, model)
}

// BuildCreateNotificationPolicyRelationshipsForTest exposes buildCreateNotificationPolicyRelationships for use in unit tests.
func BuildCreateNotificationPolicyRelationshipsForTest(ctx context.Context, plan notificationPolicyResourceModel) (*api.RelationshipsInCreateNotificationPolicyRequest, diag.Diagnostics) {
	return buildCreateNotificationPolicyRelationships(ctx, plan)
}

// BuildUpdateNotificationPolicyRelationshipsForTest exposes buildUpdateNotificationPolicyRelationships for use in unit tests.
func BuildUpdateNotificationPolicyRelationshipsForTest(ctx context.Context, plan notificationPolicyResourceModel) (*api.RelationshipsInUpdateNotificationPolicyRequest, diag.Diagnostics) {
	return buildUpdateNotificationPolicyRelationships(ctx, plan)
}

// BuildCreateNotificationPolicyAttributesForTest exposes buildCreateNotificationPolicyAttributes for use in unit tests.
func BuildCreateNotificationPolicyAttributesForTest(plan notificationPolicyResourceModel) *api.AttributesInCreateNotificationPolicyRequest {
	return buildCreateNotificationPolicyAttributes(plan)
}

// BuildUpdateNotificationPolicyAttributesForTest exposes buildUpdateNotificationPolicyAttributes for use in unit tests.
func BuildUpdateNotificationPolicyAttributesForTest(plan notificationPolicyResourceModel) *api.AttributesInUpdateNotificationPolicyRequest {
	return buildUpdateNotificationPolicyAttributes(plan)
}

// NotificationPolicyNullAttributesForTest exposes notificationPolicyNullAttributes for use in unit tests.
func NotificationPolicyNullAttributesForTest(plan notificationPolicyResourceModel) []string {
	return notificationPolicyNullAttributes(plan)
}

// NotificationPolicyReadParamsForTest exposes notificationPolicyReadParams for use in unit tests.
func NotificationPolicyReadParamsForTest() *api.GetNotificationPolicyParams {
	return notificationPolicyReadParams()
}

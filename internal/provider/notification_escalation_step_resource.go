package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &notificationEscalationStepResource{}
	_ resource.ResourceWithConfigure   = &notificationEscalationStepResource{}
	_ resource.ResourceWithImportState = &notificationEscalationStepResource{}
)

// NewNotificationEscalationStepResource returns a new notificationEscalationStepResource constructor function.
func NewNotificationEscalationStepResource() resource.Resource {
	return &notificationEscalationStepResource{}
}

// notificationEscalationStepResource implements the monotaur_notification_escalation_step managed resource.
type notificationEscalationStepResource struct {
	client *client.Client
}

// notificationEscalationStepResourceModel is the Terraform state model for an escalation step.
//
// Attribute mapping:
//
//	AttributesInCreateNotificationEscalationStepRequest: delayMinutes (required)
//	AttributesInNotificationEscalationStepResponse:      id (computed), delayMinutes,
//	                                                     createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*NotificationEscalationStepRequest:   notification_policy_id (to-one, required),
//	                                                     channel_ids (to-many, required: the API
//	                                                     rejects a step without channels with 422)
type notificationEscalationStepResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	NotificationPolicyID types.String `tfsdk:"notification_policy_id"`
	DelayMinutes         types.Int64  `tfsdk:"delay_minutes"`
	ChannelIDs           types.List   `tfsdk:"channel_ids"`
	CreateDateTime       types.String `tfsdk:"create_date_time"`
	UpdateDateTime       types.String `tfsdk:"update_date_time"`
}

// Metadata sets the resource type name.
func (r *notificationEscalationStepResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_escalation_step"
}

// Schema defines the Terraform schema for the resource.
func (r *notificationEscalationStepResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one escalation step of a Monotaur notification policy. When an alarm the policy matches " +
			"is still open and unacknowledged `delay_minutes` after it was reported, the step's channels are notified. " +
			"A policy's steps fire in order of their delay, and two steps of a policy cannot share a delay. " +
			"Deleting the policy deletes its steps.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the escalation step (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"notification_policy_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the notification policy the step belongs to. Changing it replaces the step.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"delay_minutes": schema.Int64Attribute{
				MarkdownDescription: "Minutes (1 to 10080) after the alarm was reported that the step fires. Counted from the alarm, not from the previous step; unique within the policy.",
				Required:            true,
			},
			"channel_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the notification channels the step notifies. At least one is required. " +
					"A channel only receives escalations if it subscribes to `com.monotaur.alarm.triggered`.",
				Required:    true,
				ElementType: types.StringType,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the step was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the step was last updated (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *notificationEscalationStepResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new escalation step via POST /notificationEscalationSteps.
func (r *notificationEscalationStepResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan notificationEscalationStepResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rels, diags := buildCreateNotificationEscalationStepRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateNotificationEscalationStepRequestDocument{
		Data: api.DataInCreateNotificationEscalationStepRequest{
			Type: api.ResourceTypeNotificationEscalationSteps,
			Attributes: &api.AttributesInCreateNotificationEscalationStepRequest{
				OpenapiDiscriminator: api.ResourceTypeNotificationEscalationSteps,
				DelayMinutes:         int32(plan.DelayMinutes.ValueInt64()),
			},
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_notification_escalation_step: creating escalation step", map[string]any{
		"notification_policy_id": plan.NotificationPolicyID.ValueString(),
	})

	apiResp, err := r.client.Inner().PostNotificationEscalationStepWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostNotificationEscalationStepParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Escalation Step", "Could not create escalation step: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Notification Escalation Step", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationEscalationStepResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationEscalationStep(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_escalation_step: created escalation step", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *notificationEscalationStepResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state notificationEscalationStepResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_escalation_step: reading escalation step", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetNotificationEscalationStep(ctx, id, notificationEscalationStepReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step", "Could not read escalation step "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the step is gone: deleted out-of-band, or cascade-deleted
		// with its policy. Remove it from state so the next plan re-creates it.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_escalation_step: escalation step not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationEscalationStepResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationEscalationStep(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing escalation step via PATCH /notificationEscalationSteps/{id}.
func (r *notificationEscalationStepResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan notificationEscalationStepResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state notificationEscalationStepResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	rels, diags := buildUpdateNotificationEscalationStepRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	delay := int32(plan.DelayMinutes.ValueInt64())
	body := api.UpdateNotificationEscalationStepRequestDocument{
		Data: api.DataInUpdateNotificationEscalationStepRequest{
			Id:   id,
			Type: api.ResourceTypeNotificationEscalationSteps,
			Attributes: &api.AttributesInUpdateNotificationEscalationStepRequest{
				OpenapiDiscriminator: api.ResourceTypeNotificationEscalationSteps,
				DelayMinutes:         &delay,
			},
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_notification_escalation_step: updating escalation step", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchNotificationEscalationStepWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchNotificationEscalationStepParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Escalation Step", "Could not update escalation step "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Notification Escalation Step", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetNotificationEscalationStep(ctx, id, notificationEscalationStepReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInNotificationEscalationStepResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenNotificationEscalationStep(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_notification_escalation_step: updated escalation step", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes an escalation step via DELETE /notificationEscalationSteps/{id}.
func (r *notificationEscalationStepResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notificationEscalationStepResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_escalation_step: deleting escalation step", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteNotificationEscalationStep(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Notification Escalation Step", "Could not delete escalation step "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the step is already gone, typically cascade-deleted with
		// its policy. Treat as a no-op so destroy stays idempotent.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_notification_escalation_step: escalation step already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Notification Escalation Step", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_notification_escalation_step: deleted escalation step", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_notification_escalation_step.example <id>`.
func (r *notificationEscalationStepResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// notificationEscalationStepReadParams requests the step's relationships via
// `?include=` so the response carries their `data`; a bare GET echoes `links`
// only, which would leave an imported step without notification_policy_id or
// channel_ids. The included channels are scoped to the maximum page size.
func notificationEscalationStepReadParams() *api.GetNotificationEscalationStepParams {
	include := "notificationPolicy,channels"
	pageSize := "channels:500"
	query := map[string]*string{"include": &include, "page[size]": &pageSize}
	return &api.GetNotificationEscalationStepParams{Query: &query}
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

func notificationEscalationStepPolicy(plan notificationEscalationStepResourceModel) api.ToOneNotificationPolicyInRequest {
	return api.ToOneNotificationPolicyInRequest{
		Data: api.NotificationPolicyIdentifierInRequest{
			Id:   plan.NotificationPolicyID.ValueString(),
			Type: api.ResourceTypeNotificationPolicies,
		},
	}
}

// buildCreateNotificationEscalationStepRelationships converts the plan's
// relationship ID fields into the API request type for create operations.
func buildCreateNotificationEscalationStepRelationships(ctx context.Context, plan notificationEscalationStepResourceModel) (*api.RelationshipsInCreateNotificationEscalationStepRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInCreateNotificationEscalationStepRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationEscalationSteps,
		NotificationPolicy:   notificationEscalationStepPolicy(plan),
	}
	ids, ok, diags := policyRelKnownIDs(ctx, plan.ChannelIDs)
	if ok {
		rels.Channels = policyRelChannelIdentifiers(ids)
	}
	return rels, diags
}

// buildUpdateNotificationEscalationStepRelationships converts the plan's
// relationship ID fields into the API request type for update operations.
// notification_policy_id forces replacement, so it is not sent on update.
func buildUpdateNotificationEscalationStepRelationships(ctx context.Context, plan notificationEscalationStepResourceModel) (*api.RelationshipsInUpdateNotificationEscalationStepRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInUpdateNotificationEscalationStepRequest{
		OpenapiDiscriminator: api.ResourceTypeNotificationEscalationSteps,
	}
	ids, ok, diags := policyRelKnownIDs(ctx, plan.ChannelIDs)
	if ok {
		rels.Channels = policyRelChannelIdentifiers(ids)
	}
	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenNotificationEscalationStep maps the API DataInNotificationEscalationStepResponse onto the Terraform state model.
func flattenNotificationEscalationStep(ctx context.Context, data api.DataInNotificationEscalationStepResponse, model *notificationEscalationStepResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.DelayMinutes != nil {
			model.DelayMinutes = types.Int64Value(int64(*attrs.DelayMinutes))
		} else {
			model.DelayMinutes = types.Int64Null()
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
		rels = &api.RelationshipsInNotificationEscalationStepResponse{}
	}

	// notification_policy_id — to-one. Preserve the prior value when the API
	// echoes only `links` (no embedded `data`).
	if rels.NotificationPolicy != nil && rels.NotificationPolicy.Data != nil {
		model.NotificationPolicyID = types.StringValue(rels.NotificationPolicy.Data.Id)
	} else if model.NotificationPolicyID.IsUnknown() {
		model.NotificationPolicyID = types.StringNull()
	}

	// channel_ids — to-many.
	var ids []string
	present := rels.Channels != nil && rels.Channels.Data != nil
	if present {
		for _, item := range *rels.Channels.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d := policyRelFlattenIDList(ctx, model.ChannelIDs, ids, present)
	diags.Append(d...)
	model.ChannelIDs = list

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// NotificationEscalationStepResourceModelForTest is a type alias for
// notificationEscalationStepResourceModel for use in unit tests.
type NotificationEscalationStepResourceModelForTest = notificationEscalationStepResourceModel

// FlattenNotificationEscalationStepForTest exposes flattenNotificationEscalationStep for use in unit tests.
func FlattenNotificationEscalationStepForTest(ctx context.Context, data api.DataInNotificationEscalationStepResponse, model *notificationEscalationStepResourceModel) diag.Diagnostics {
	return flattenNotificationEscalationStep(ctx, data, model)
}

// BuildCreateNotificationEscalationStepRelationshipsForTest exposes buildCreateNotificationEscalationStepRelationships for use in unit tests.
func BuildCreateNotificationEscalationStepRelationshipsForTest(ctx context.Context, plan notificationEscalationStepResourceModel) (*api.RelationshipsInCreateNotificationEscalationStepRequest, diag.Diagnostics) {
	return buildCreateNotificationEscalationStepRelationships(ctx, plan)
}

// BuildUpdateNotificationEscalationStepRelationshipsForTest exposes buildUpdateNotificationEscalationStepRelationships for use in unit tests.
func BuildUpdateNotificationEscalationStepRelationshipsForTest(ctx context.Context, plan notificationEscalationStepResourceModel) (*api.RelationshipsInUpdateNotificationEscalationStepRequest, diag.Diagnostics) {
	return buildUpdateNotificationEscalationStepRelationships(ctx, plan)
}

// NotificationEscalationStepReadParamsForTest exposes notificationEscalationStepReadParams for use in unit tests.
func NotificationEscalationStepReadParamsForTest() *api.GetNotificationEscalationStepParams {
	return notificationEscalationStepReadParams()
}

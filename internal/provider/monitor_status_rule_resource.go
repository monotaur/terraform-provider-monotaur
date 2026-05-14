package provider

import (
	"context"
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
	_ resource.Resource                = &monitorStatusRuleResource{}
	_ resource.ResourceWithConfigure   = &monitorStatusRuleResource{}
	_ resource.ResourceWithImportState = &monitorStatusRuleResource{}
)

// NewMonitorStatusRuleResource returns a new monitorStatusRuleResource constructor function.
func NewMonitorStatusRuleResource() resource.Resource {
	return &monitorStatusRuleResource{}
}

// monitorStatusRuleResource implements the monotaur_monitor_status_rule managed resource.
type monitorStatusRuleResource struct {
	client *client.Client
}

// monitorStatusRuleResourceModel is the Terraform state model for a monitor status rule.
//
// Attribute mapping:
//
//	AttributesInCreateMonitorStatusRuleRequest:  predicate (required), status (optional), statusMessage (optional)
//	AttributesInMonitorStatusRuleResponse:       id (computed), predicate, status, statusMessage,
//	                                              createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*MonitorStatusRuleRequest:    monitor_id (to-one, required on create)
type monitorStatusRuleResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Predicate      types.String `tfsdk:"predicate"`
	Status         types.String `tfsdk:"status"`
	StatusMessage  types.String `tfsdk:"status_message"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
	MonitorID      types.String `tfsdk:"monitor_id"`
}

// Metadata sets the resource type name.
func (r *monitorStatusRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_status_rule"
}

// Schema defines the Terraform schema for the resource.
func (r *monitorStatusRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur monitor status rule. Monitor status rules define conditions that set the status of a monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the monitor status rule (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"predicate": schema.StringAttribute{
				MarkdownDescription: "The predicate expression that determines when the rule applies.",
				Required:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The monitor status to apply when the predicate matches.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status_message": schema.StringAttribute{
				MarkdownDescription: "A message to accompany the status when the predicate matches.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor status rule was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor status rule was last updated (assigned by the API).",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this status rule belongs to.",
				Required:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *monitorStatusRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new monitor status rule via POST /monitorStatusRules.
func (r *monitorStatusRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorStatusRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateMonitorStatusRuleRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitorStatusRules,
		Predicate:            plan.Predicate.ValueString(),
	}

	if !plan.Status.IsNull() && !plan.Status.IsUnknown() {
		s := api.MonitorStatus(plan.Status.ValueString())
		attrs.Status = &s
	}

	if !plan.StatusMessage.IsNull() && !plan.StatusMessage.IsUnknown() {
		sm := plan.StatusMessage.ValueString()
		attrs.StatusMessage = &sm
	}

	rels, diags := buildCreateMonitorStatusRuleRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateMonitorStatusRuleRequestDocument{
		Data: api.DataInCreateMonitorStatusRuleRequest{
			Type:          api.ResourceTypeMonitorStatusRules,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_monitor_status_rule: creating monitor status rule", map[string]any{"predicate": plan.Predicate.ValueString()})

	apiResp, err := r.client.Inner().PostMonitorStatusRuleWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostMonitorStatusRuleParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Monitor Status Rule", "Could not create monitor status rule: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Monitor Status Rule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorStatusRuleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitorStatusRule(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_monitor_status_rule: created monitor status rule", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *monitorStatusRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorStatusRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor_status_rule: reading monitor status rule", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetMonitorStatusRule(ctx, id, &api.GetMonitorStatusRuleParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule", "Could not read monitor status rule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorStatusRuleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitorStatusRule(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing monitor status rule via PATCH /monitorStatusRules/{id}.
func (r *monitorStatusRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan monitorStatusRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state monitorStatusRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	predicateVal := plan.Predicate.ValueString()
	attrs := &api.AttributesInUpdateMonitorStatusRuleRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitorStatusRules,
		Predicate:            &predicateVal,
	}

	if !plan.Status.IsNull() && !plan.Status.IsUnknown() {
		s := api.MonitorStatus(plan.Status.ValueString())
		attrs.Status = &s
	}

	if !plan.StatusMessage.IsNull() && !plan.StatusMessage.IsUnknown() {
		sm := plan.StatusMessage.ValueString()
		attrs.StatusMessage = &sm
	}

	rels, diags := buildUpdateMonitorStatusRuleRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateMonitorStatusRuleRequestDocument{
		Data: api.DataInUpdateMonitorStatusRuleRequest{
			Id:            id,
			Type:          api.ResourceTypeMonitorStatusRules,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_monitor_status_rule: updating monitor status rule", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchMonitorStatusRuleWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchMonitorStatusRuleParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Monitor Status Rule", "Could not update monitor status rule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Monitor Status Rule", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetMonitorStatusRule(ctx, id, &api.GetMonitorStatusRuleParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInMonitorStatusRuleResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitorStatusRule(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_monitor_status_rule: updated monitor status rule", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a monitor status rule via DELETE /monitorStatusRules/{id}.
func (r *monitorStatusRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorStatusRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor_status_rule: deleting monitor status rule", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteMonitorStatusRule(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Monitor Status Rule", "Could not delete monitor status rule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Monitor Status Rule", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_monitor_status_rule: deleted monitor status rule", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_monitor_status_rule.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *monitorStatusRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateMonitorStatusRuleRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateMonitorStatusRuleRelationships(_ context.Context, plan monitorStatusRuleResourceModel) (*api.RelationshipsInCreateMonitorStatusRuleRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateMonitorStatusRuleRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitorStatusRules,
		Monitor: api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		},
	}

	return rels, diags
}

// buildUpdateMonitorStatusRuleRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateMonitorStatusRuleRelationships(_ context.Context, plan monitorStatusRuleResourceModel) (*api.RelationshipsInUpdateMonitorStatusRuleRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInUpdateMonitorStatusRuleRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitorStatusRules,
	}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenMonitorStatusRule maps the API DataInMonitorStatusRuleResponse onto the Terraform state model.
func flattenMonitorStatusRule(_ context.Context, data api.DataInMonitorStatusRuleResponse, model *monitorStatusRuleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Predicate != nil {
			model.Predicate = types.StringValue(*attrs.Predicate)
		} else {
			model.Predicate = types.StringNull()
		}

		if attrs.Status != nil {
			model.Status = types.StringValue(string(*attrs.Status))
		} else {
			model.Status = types.StringNull()
		}

		if attrs.StatusMessage != nil {
			model.StatusMessage = types.StringValue(*attrs.StatusMessage)
		} else {
			model.StatusMessage = types.StringNull()
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

	// monitor_id — to-one. Preserve the prior model value when the API echoes
	// only `links` (no embedded `data`); overwriting with null would cause
	// "was X, but now null" inconsistency errors after a successful create.
	rels := data.Relationships
	if rels != nil && rels.Monitor != nil && rels.Monitor.Data != nil {
		model.MonitorID = types.StringValue(rels.Monitor.Data.Id)
	} else if model.MonitorID.IsUnknown() {
		model.MonitorID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// MonitorStatusRuleResourceModelForTest is a type alias for monitorStatusRuleResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type MonitorStatusRuleResourceModelForTest = monitorStatusRuleResourceModel

// FlattenMonitorStatusRuleForTest exposes flattenMonitorStatusRule for use in unit tests.
func FlattenMonitorStatusRuleForTest(ctx context.Context, data api.DataInMonitorStatusRuleResponse, model *monitorStatusRuleResourceModel) diag.Diagnostics {
	return flattenMonitorStatusRule(ctx, data, model)
}

// BuildCreateMonitorStatusRuleRelationshipsForTest exposes buildCreateMonitorStatusRuleRelationships for use in unit tests.
func BuildCreateMonitorStatusRuleRelationshipsForTest(ctx context.Context, plan monitorStatusRuleResourceModel) (*api.RelationshipsInCreateMonitorStatusRuleRequest, diag.Diagnostics) {
	return buildCreateMonitorStatusRuleRelationships(ctx, plan)
}

// BuildUpdateMonitorStatusRuleRelationshipsForTest exposes buildUpdateMonitorStatusRuleRelationships for use in unit tests.
func BuildUpdateMonitorStatusRuleRelationshipsForTest(ctx context.Context, plan monitorStatusRuleResourceModel) (*api.RelationshipsInUpdateMonitorStatusRuleRequest, diag.Diagnostics) {
	return buildUpdateMonitorStatusRuleRelationships(ctx, plan)
}

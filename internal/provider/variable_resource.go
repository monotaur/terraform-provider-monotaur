package provider

import (
	"context"
	"fmt"
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
	_ resource.Resource                = &variableResource{}
	_ resource.ResourceWithConfigure   = &variableResource{}
	_ resource.ResourceWithImportState = &variableResource{}
)

// NewVariableResource returns a new variableResource constructor function.
func NewVariableResource() resource.Resource {
	return &variableResource{}
}

// variableResource implements the monotaur_variable managed resource.
type variableResource struct {
	client *client.Client
}

// variableResourceModel is the Terraform state model for a variable.
//
// Attribute mapping:
//
//	AttributesInCreateVariableRequest:  name (required), value (required), description (optional)
//	AttributesInVariableResponse:       id (computed), name, value, description,
//	                                     createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*VariableRequest:    monitor_id (to-one, optional/nullable)
type variableResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
	MonitorID      types.String `tfsdk:"monitor_id"`
}

// Metadata sets the resource type name.
func (r *variableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable"
}

// Schema defines the Terraform schema for the resource.
func (r *variableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur variable. Variables are named key-value pairs that can optionally be associated with a monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the variable (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the variable.",
				Required:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value of the variable.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the variable.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the variable was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the variable was last updated (assigned by the API).",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this variable belongs to (optional).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *variableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new variable via POST /variables.
func (r *variableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan variableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateVariableRequest{
		Name:  plan.Name.ValueString(),
		Value: plan.Value.ValueString(),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	rels, diags := buildCreateVariableRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateVariableRequestDocument{
		Data: api.DataInCreateVariableRequest{
			Type:          api.ResourceTypeVariables,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_variable: creating variable", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostVariableWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostVariableParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Variable", "Could not create variable: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Variable", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInVariableResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Variable Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenVariable(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_variable: created variable", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *variableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state variableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_variable: reading variable", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetVariable(ctx, id, &api.GetVariableParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Variable", "Could not read variable "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Variable", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInVariableResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Variable Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenVariable(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing variable via PATCH /variables/{id}.
func (r *variableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan variableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state variableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateVariableRequest{}

	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		attrs.Name = &v
	}

	if !plan.Value.IsNull() && !plan.Value.IsUnknown() {
		v := plan.Value.ValueString()
		attrs.Value = &v
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	rels, diags := buildUpdateVariableRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateVariableRequestDocument{
		Data: api.DataInUpdateVariableRequest{
			Id:            id,
			Type:          api.ResourceTypeVariables,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_variable: updating variable", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchVariableWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchVariableParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Variable", "Could not update variable "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Variable", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInVariableResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Variable Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenVariable(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_variable: updated variable", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a variable via DELETE /variables/{id}.
func (r *variableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state variableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_variable: deleting variable", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteVariable(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Variable", "Could not delete variable "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Variable", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_variable: deleted variable", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_variable.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *variableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateVariableRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateVariableRelationships(_ context.Context, plan variableResourceModel) (*api.RelationshipsInCreateVariableRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateVariableRequest{}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.NullableToOneMonitorInRequest{
			Data: &api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		}
	}

	return rels, diags
}

// buildUpdateVariableRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateVariableRelationships(_ context.Context, plan variableResourceModel) (*api.RelationshipsInUpdateVariableRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInUpdateVariableRequest{}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.NullableToOneMonitorInRequest{
			Data: &api.MonitorIdentifierInRequest{
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

// flattenVariable maps the API DataInVariableResponse onto the Terraform state model.
func flattenVariable(_ context.Context, data api.DataInVariableResponse, model *variableResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Value != nil {
			model.Value = types.StringValue(*attrs.Value)
		} else {
			model.Value = types.StringNull()
		}

		if attrs.Description != nil {
			model.Description = types.StringValue(*attrs.Description)
		} else {
			model.Description = types.StringNull()
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

	if data.Relationships != nil {
		rels := data.Relationships

		// monitor_id — nullable to-one
		if rels.Monitor != nil && rels.Monitor.Data != nil {
			model.MonitorID = types.StringValue(rels.Monitor.Data.Id)
		} else {
			model.MonitorID = types.StringNull()
		}
	} else {
		model.MonitorID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// VariableResourceModelForTest is a type alias for variableResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type VariableResourceModelForTest = variableResourceModel

// FlattenVariableForTest exposes flattenVariable for use in unit tests.
func FlattenVariableForTest(ctx context.Context, data api.DataInVariableResponse, model *variableResourceModel) diag.Diagnostics {
	return flattenVariable(ctx, data, model)
}

// BuildCreateVariableRelationshipsForTest exposes buildCreateVariableRelationships for use in unit tests.
func BuildCreateVariableRelationshipsForTest(ctx context.Context, plan variableResourceModel) (*api.RelationshipsInCreateVariableRequest, diag.Diagnostics) {
	return buildCreateVariableRelationships(ctx, plan)
}

// BuildUpdateVariableRelationshipsForTest exposes buildUpdateVariableRelationships for use in unit tests.
func BuildUpdateVariableRelationshipsForTest(ctx context.Context, plan variableResourceModel) (*api.RelationshipsInUpdateVariableRequest, diag.Diagnostics) {
	return buildUpdateVariableRelationships(ctx, plan)
}

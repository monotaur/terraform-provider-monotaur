package provider

import (
	"context"
	"fmt"
	"net/http"

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
	_ resource.Resource                = &roleResource{}
	_ resource.ResourceWithConfigure   = &roleResource{}
	_ resource.ResourceWithImportState = &roleResource{}
)

// NewRoleResource returns a new roleResource constructor function.
func NewRoleResource() resource.Resource {
	return &roleResource{}
}

// roleResource implements the monotaur_role managed resource.
type roleResource struct {
	client *client.Client
}

// roleResourceModel is the Terraform state model for a role.
//
// Attribute mapping:
//
//	AttributesInCreateAdminRoleRequest:  name (required), description (optional), permissions (required)
//	AttributesInAdminRoleResponse:       id (computed), name, description, permissions
type roleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Permissions types.List   `tfsdk:"permissions"`
}

// Metadata sets the resource type name.
func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the Terraform schema for the resource.
func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur role. Roles define a named set of permissions that can be assigned to service accounts.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the role (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the role.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the role.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"permissions": schema.ListAttribute{
				MarkdownDescription: "The list of permission strings granted by this role.",
				Required:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new role via POST /admin/roles.
func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	permissions, diags := toStringSlice(ctx, plan.Permissions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateAdminRoleRequest{
		OpenapiDiscriminator: api.ResourceTypeAdminRoles,
		Name:                 plan.Name.ValueString(),
		Permissions:          permissions,
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	body := api.CreateAdminRoleRequestDocument{
		Data: api.DataInCreateAdminRoleRequest{
			Type:       api.ResourceTypeAdminRoles,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_role: creating role", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostAdminRoleWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAdminRoleParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Role", "Could not create role: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Role", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminRoleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenRole(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_role: created role", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role: reading role", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetAdminRole(ctx, id, &api.GetAdminRoleParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role", "Could not read role "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Role", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminRoleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenRole(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing role via PATCH /admin/roles/{id}.
func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateAdminRoleRequest{
		OpenapiDiscriminator: api.ResourceTypeAdminRoles,
	}

	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		attrs.Name = &v
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	if !plan.Permissions.IsNull() && !plan.Permissions.IsUnknown() {
		permissions, diags := toStringSlice(ctx, plan.Permissions)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.Permissions = &permissions
	}

	body := api.UpdateAdminRoleRequestDocument{
		Data: api.DataInUpdateAdminRoleRequest{
			Id:         id,
			Type:       api.ResourceTypeAdminRoles,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_role: updating role", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchAdminRoleWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchAdminRoleParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Role", "Could not update role "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Role", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetAdminRole(ctx, id, &api.GetAdminRoleParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInAdminRoleResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenRole(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_role: updated role", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a role via DELETE /admin/roles/{id}.
func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role: deleting role", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteAdminRole(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Role", "Could not delete role "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Role", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_role: deleted role", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_role.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenRole maps the API DataInAdminRoleResponse onto the Terraform state model.
func flattenRole(ctx context.Context, data api.DataInAdminRoleResponse, model *roleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Description != nil {
			model.Description = types.StringValue(*attrs.Description)
		} else {
			model.Description = types.StringNull()
		}

		if attrs.Permissions != nil {
			listVal, d := types.ListValueFrom(ctx, types.StringType, *attrs.Permissions)
			diags.Append(d...)
			if !d.HasError() {
				model.Permissions = listVal
			}
		} else {
			model.Permissions = types.ListValueMust(types.StringType, nil)
		}
	}

	return diags
}

// toStringSlice converts a types.List of strings into a []string.
func toStringSlice(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var out []string
	diags := list.ElementsAs(ctx, &out, false)
	return out, diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// RoleResourceModelForTest is a type alias for roleResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type RoleResourceModelForTest = roleResourceModel

// FlattenRoleForTest exposes flattenRole for use in unit tests.
func FlattenRoleForTest(ctx context.Context, data api.DataInAdminRoleResponse, model *roleResourceModel) diag.Diagnostics {
	return flattenRole(ctx, data, model)
}

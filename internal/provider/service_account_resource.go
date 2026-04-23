package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &serviceAccountResource{}
	_ resource.ResourceWithConfigure   = &serviceAccountResource{}
	_ resource.ResourceWithImportState = &serviceAccountResource{}
)

// NewServiceAccountResource returns a new serviceAccountResource constructor function.
func NewServiceAccountResource() resource.Resource {
	return &serviceAccountResource{}
}

// serviceAccountResource implements the monotaur_service_account managed resource.
type serviceAccountResource struct {
	client *client.Client
}

// serviceAccountResourceModel is the Terraform state model for a service account.
//
// Attribute mapping:
//
//	AttributesInCreateAdminServiceAccountRequest:  name (required), description (optional), disabled (optional)
//	AttributesInAdminServiceAccountResponse:       id (computed), name, description, disabled,
//	                                                createdAt (computed)
type serviceAccountResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Disabled    types.Bool   `tfsdk:"disabled"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

// Metadata sets the resource type name.
func (r *serviceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

// Schema defines the Terraform schema for the resource.
func (r *serviceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur service account. Service accounts are non-human identities used for programmatic API access.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the service account (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the service account.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the service account.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"disabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the service account is disabled. Defaults to false.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the service account was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *serviceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new service account via POST /admin/serviceAccounts.
func (r *serviceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateAdminServiceAccountRequest{
		Name: plan.Name.ValueString(),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	if !plan.Disabled.IsNull() && !plan.Disabled.IsUnknown() {
		v := plan.Disabled.ValueBool()
		attrs.Disabled = &v
	}

	body := api.CreateAdminServiceAccountRequestDocument{
		Data: api.DataInCreateAdminServiceAccountRequest{
			Type:       api.ResourceTypeAdminServiceAccounts,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_service_account: creating service account", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostAdminServiceAccountWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAdminServiceAccountParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Service Account", "Could not create service account: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Service Account", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminServiceAccountResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenServiceAccount(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_service_account: created service account", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *serviceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_service_account: reading service account", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetAdminServiceAccount(ctx, id, &api.GetAdminServiceAccountParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account", "Could not read service account "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminServiceAccountResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenServiceAccount(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing service account via PATCH /admin/serviceAccounts/{id}.
func (r *serviceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateAdminServiceAccountRequest{}

	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		attrs.Name = &v
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	if !plan.Disabled.IsNull() && !plan.Disabled.IsUnknown() {
		v := plan.Disabled.ValueBool()
		attrs.Disabled = &v
	}

	body := api.UpdateAdminServiceAccountRequestDocument{
		Data: api.DataInUpdateAdminServiceAccountRequest{
			Id:         id,
			Type:       api.ResourceTypeAdminServiceAccounts,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_service_account: updating service account", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchAdminServiceAccountWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchAdminServiceAccountParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Service Account", "Could not update service account "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Service Account", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminServiceAccountResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenServiceAccount(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_service_account: updated service account", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a service account via DELETE /admin/serviceAccounts/{id}.
func (r *serviceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_service_account: deleting service account", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteAdminServiceAccount(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Service Account", "Could not delete service account "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Service Account", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_service_account: deleted service account", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_service_account.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *serviceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenServiceAccount maps the API DataInAdminServiceAccountResponse onto the Terraform state model.
func flattenServiceAccount(_ context.Context, data api.DataInAdminServiceAccountResponse, model *serviceAccountResourceModel) diag.Diagnostics {
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

		if attrs.Disabled != nil {
			model.Disabled = types.BoolValue(*attrs.Disabled)
		} else {
			model.Disabled = types.BoolNull()
		}

		if attrs.CreatedAt != nil {
			model.CreatedAt = types.StringValue(attrs.CreatedAt.Format(time.RFC3339))
		} else {
			model.CreatedAt = types.StringNull()
		}
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// ServiceAccountResourceModelForTest is a type alias for serviceAccountResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type ServiceAccountResourceModelForTest = serviceAccountResourceModel

// FlattenServiceAccountForTest exposes flattenServiceAccount for use in unit tests.
func FlattenServiceAccountForTest(ctx context.Context, data api.DataInAdminServiceAccountResponse, model *serviceAccountResourceModel) diag.Diagnostics {
	return flattenServiceAccount(ctx, data, model)
}

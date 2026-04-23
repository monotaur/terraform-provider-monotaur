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
	_ resource.Resource                = &roleAssignmentResource{}
	_ resource.ResourceWithConfigure   = &roleAssignmentResource{}
	_ resource.ResourceWithImportState = &roleAssignmentResource{}
)

// NewRoleAssignmentResource returns a new roleAssignmentResource constructor function.
func NewRoleAssignmentResource() resource.Resource {
	return &roleAssignmentResource{}
}

// roleAssignmentResource implements the monotaur_role_assignment managed resource.
type roleAssignmentResource struct {
	client *client.Client
}

// roleAssignmentResourceModel is the Terraform state model for a role assignment.
//
// Attribute mapping:
//
//	RelationshipsInCreateAdminRoleAssignmentRequest: role_id (required), service_account_id (required)
//	AttributesInAdminRoleAssignmentResponse:         assigned_at (computed)
//	DataInAdminRoleAssignmentResponse:               id (computed)
type roleAssignmentResourceModel struct {
	ID               types.String `tfsdk:"id"`
	RoleID           types.String `tfsdk:"role_id"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	AssignedAt       types.String `tfsdk:"assigned_at"`
}

// Metadata sets the resource type name.
func (r *roleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

// Schema defines the Terraform schema for the resource.
func (r *roleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur role assignment. A role assignment binds a role to a service account, granting the service account all permissions defined by the role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the role assignment (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the role to assign.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the service account to assign the role to.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"assigned_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the role was assigned (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new role assignment via POST /admin/roleAssignments.
func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateAdminRoleAssignmentRequestDocument{
		Data: api.DataInCreateAdminRoleAssignmentRequest{
			Type: api.ResourceTypeAdminRoleAssignments,
			Relationships: &api.RelationshipsInCreateAdminRoleAssignmentRequest{
				Role: api.ToOneAdminRoleInRequest{
					Data: api.AdminRoleIdentifierInRequest{
						Id:   plan.RoleID.ValueString(),
						Type: api.ResourceTypeAdminRoles,
					},
				},
				ServiceAccount: api.ToOneAdminServiceAccountInRequest{
					Data: api.AdminServiceAccountIdentifierInRequest{
						Id:   plan.ServiceAccountID.ValueString(),
						Type: api.ResourceTypeAdminServiceAccounts,
					},
				},
			},
		},
	}

	tflog.Debug(ctx, "monotaur_role_assignment: creating role assignment", map[string]any{
		"role_id":            plan.RoleID.ValueString(),
		"service_account_id": plan.ServiceAccountID.ValueString(),
	})

	apiResp, err := r.client.Inner().PostAdminRoleAssignmentWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAdminRoleAssignmentParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Role Assignment", "Could not create role assignment: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Role Assignment", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminRoleAssignmentResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Assignment Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenRoleAssignment(data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_role_assignment: created role assignment", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role_assignment: reading role assignment", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetAdminRoleAssignment(ctx, id, &api.GetAdminRoleAssignmentParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Assignment", "Could not read role assignment "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Role Assignment", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminRoleAssignmentResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Role Assignment Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenRoleAssignment(data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is not supported for role assignments. The role_id and service_account_id
// attributes are both marked RequiresReplace, so any change triggers a replacement.
// This method satisfies the resource.Resource interface but should never be called
// in practice.
func (r *roleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Role Assignment Cannot Be Updated",
		"Role assignments are immutable. Changing role_id or service_account_id requires destroying and recreating the resource.",
	)
}

// Delete removes a role assignment via DELETE /admin/roleAssignments/{id}.
func (r *roleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role_assignment: deleting role assignment", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteAdminRoleAssignment(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Role Assignment", "Could not delete role assignment "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Role Assignment", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_role_assignment: deleted role assignment", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_role_assignment.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenRoleAssignment maps the API DataInAdminRoleAssignmentResponse onto the Terraform state model.
func flattenRoleAssignment(data api.DataInAdminRoleAssignmentResponse, model *roleAssignmentResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes
		if attrs.AssignedAt != nil {
			model.AssignedAt = types.StringValue(attrs.AssignedAt.Format(time.RFC3339))
		} else {
			model.AssignedAt = types.StringNull()
		}
	}

	if data.Relationships != nil {
		rels := data.Relationships
		if rels.Role != nil && rels.Role.Data != nil {
			model.RoleID = types.StringValue(rels.Role.Data.Id)
		} else {
			model.RoleID = types.StringNull()
		}

		if rels.ServiceAccount != nil && rels.ServiceAccount.Data != nil {
			model.ServiceAccountID = types.StringValue(rels.ServiceAccount.Data.Id)
		} else {
			model.ServiceAccountID = types.StringNull()
		}
	}

	return diags
}

// ---------------------------------------------------------------------------
// Relationship builder helpers (exported for testing)
// ---------------------------------------------------------------------------

// BuildRoleAssignmentRelationships constructs the RelationshipsInCreateAdminRoleAssignmentRequest
// from individual role and service account IDs.
func BuildRoleAssignmentRelationships(roleID, serviceAccountID string) *api.RelationshipsInCreateAdminRoleAssignmentRequest {
	return &api.RelationshipsInCreateAdminRoleAssignmentRequest{
		Role: api.ToOneAdminRoleInRequest{
			Data: api.AdminRoleIdentifierInRequest{
				Id:   roleID,
				Type: api.ResourceTypeAdminRoles,
			},
		},
		ServiceAccount: api.ToOneAdminServiceAccountInRequest{
			Data: api.AdminServiceAccountIdentifierInRequest{
				Id:   serviceAccountID,
				Type: api.ResourceTypeAdminServiceAccounts,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// RoleAssignmentResourceModelForTest is a type alias for roleAssignmentResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type RoleAssignmentResourceModelForTest = roleAssignmentResourceModel

// FlattenRoleAssignmentForTest exposes flattenRoleAssignment for use in unit tests.
func FlattenRoleAssignmentForTest(data api.DataInAdminRoleAssignmentResponse, model *roleAssignmentResourceModel) diag.Diagnostics {
	return flattenRoleAssignment(data, model)
}

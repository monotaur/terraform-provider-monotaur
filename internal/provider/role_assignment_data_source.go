package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ datasource.DataSource              = &roleAssignmentDataSource{}
	_ datasource.DataSourceWithConfigure = &roleAssignmentDataSource{}
)

// NewRoleAssignmentDataSource returns a new roleAssignmentDataSource constructor function.
func NewRoleAssignmentDataSource() datasource.DataSource {
	return &roleAssignmentDataSource{}
}

// roleAssignmentDataSource implements the data "monotaur_role_assignment" data source.
type roleAssignmentDataSource struct {
	client *client.Client
}

// roleAssignmentDataSourceModel is an alias for roleAssignmentResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type roleAssignmentDataSourceModel = roleAssignmentResourceModel

// Metadata sets the data source type name.
func (d *roleAssignmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

// Schema defines the Terraform schema for the data source.
func (d *roleAssignmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur role assignment by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the role assignment to look up.",
				Required:            true,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the role that is assigned.",
				Computed:            true,
			},
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the service account the role is assigned to.",
				Computed:            true,
			},
			"assigned_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the role was assigned.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *roleAssignmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = c
}

// Read fetches the role assignment by ID from the API.
func (d *roleAssignmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config roleAssignmentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role_assignment data source: reading role assignment", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAdminRoleAssignment(ctx, id, &api.GetAdminRoleAssignmentParams{})
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

	// Re-use the flatten helper. roleAssignmentDataSourceModel is an alias of
	// roleAssignmentResourceModel so they are interchangeable here.
	var state roleAssignmentDataSourceModel
	resp.Diagnostics.Append(flattenRoleAssignment(data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

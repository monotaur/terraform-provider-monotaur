package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ datasource.DataSource              = &roleDataSource{}
	_ datasource.DataSourceWithConfigure = &roleDataSource{}
)

// NewRoleDataSource returns a new roleDataSource constructor function.
func NewRoleDataSource() datasource.DataSource {
	return &roleDataSource{}
}

// roleDataSource implements the data "monotaur_role" data source.
type roleDataSource struct {
	client *client.Client
}

// roleDataSourceModel is an alias for roleResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type roleDataSourceModel = roleResourceModel

// Metadata sets the data source type name.
func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the Terraform schema for the data source.
func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur role by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the role to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the role.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the role.",
				Computed:            true,
			},
			"permissions": schema.ListAttribute{
				MarkdownDescription: "The list of permission strings granted by this role.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *roleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the role by ID from the API.
func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config roleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_role data source: reading role", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAdminRole(ctx, id, &api.GetAdminRoleParams{})
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

	// Re-use the flatten helper. roleDataSourceModel is an alias of
	// roleResourceModel so they are interchangeable here.
	var state roleDataSourceModel
	resp.Diagnostics.Append(flattenRole(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

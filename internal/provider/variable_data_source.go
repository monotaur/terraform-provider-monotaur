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
	_ datasource.DataSource              = &variableDataSource{}
	_ datasource.DataSourceWithConfigure = &variableDataSource{}
)

// NewVariableDataSource returns a new variableDataSource constructor function.
func NewVariableDataSource() datasource.DataSource {
	return &variableDataSource{}
}

// variableDataSource implements the data "monotaur_variable" data source.
type variableDataSource struct {
	client *client.Client
}

// variableDataSourceModel is an alias for variableResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type variableDataSourceModel = variableResourceModel

// Metadata sets the data source type name.
func (d *variableDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable"
}

// Schema defines the Terraform schema for the data source.
func (d *variableDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur variable by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the variable to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the variable.",
				Computed:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value of the variable.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the variable.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the variable was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the variable was last updated.",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this variable belongs to.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *variableDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the variable by ID from the API.
func (d *variableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config variableDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_variable data source: reading variable", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetVariable(ctx, id, &api.GetVariableParams{})
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

	// Re-use the flatten helper. variableDataSourceModel is an alias of
	// variableResourceModel so they are interchangeable here.
	var state variableDataSourceModel
	resp.Diagnostics.Append(flattenVariable(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

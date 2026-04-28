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
	_ datasource.DataSource              = &componentDataSource{}
	_ datasource.DataSourceWithConfigure = &componentDataSource{}
)

// NewComponentDataSource returns a new componentDataSource constructor function.
func NewComponentDataSource() datasource.DataSource {
	return &componentDataSource{}
}

// componentDataSource implements the data "monotaur_component" data source.
type componentDataSource struct {
	client *client.Client
}

// componentDataSourceModel is an alias for componentResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type componentDataSourceModel = componentResourceModel

// Metadata sets the data source type name.
func (d *componentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_component"
}

// Schema defines the Terraform schema for the data source.
func (d *componentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur component by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the component to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the component.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the component was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the component was last updated.",
				Computed:            true,
			},
			"business_hours_id": schema.StringAttribute{
				MarkdownDescription: "ID of the business hours schedule associated with this component.",
				Computed:            true,
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels associated with this component.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"maintenance_window_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of maintenance windows associated with this component.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of monitors associated with this component.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *componentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the component by ID from the API.
func (d *componentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config componentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_component data source: reading component", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetComponent(ctx, id, &api.GetComponentParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component", "Could not read component "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Component", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInComponentResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component Response", err.Error())
		return
	}

	// Re-use the flatten helper. componentDataSourceModel is an alias of
	// componentResourceModel so they are interchangeable here.
	var state componentDataSourceModel
	resp.Diagnostics.Append(flattenComponent(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

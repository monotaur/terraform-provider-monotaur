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
	_ datasource.DataSource              = &monitorDataSource{}
	_ datasource.DataSourceWithConfigure = &monitorDataSource{}
)

// NewMonitorDataSource returns a new monitorDataSource constructor function.
func NewMonitorDataSource() datasource.DataSource {
	return &monitorDataSource{}
}

// monitorDataSource implements the data "monotaur_monitor" data source.
type monitorDataSource struct {
	client *client.Client
}

// monitorDataSourceModel is an alias for monitorResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type monitorDataSourceModel = monitorResourceModel

// Metadata sets the data source type name.
func (d *monitorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

// Schema defines the Terraform schema for the data source.
func (d *monitorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur monitor by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the monitor to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the monitor.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The monitor type (e.g. `Alert`, `Heartbeat`).",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor was last updated.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The current status of the monitor (system-managed, read-only).",
				Computed:            true,
			},
			"status_message": schema.StringAttribute{
				MarkdownDescription: "A message describing the current status (system-managed, read-only).",
				Computed:            true,
			},
			"status_expiration_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the current status expires (system-managed, read-only).",
				Computed:            true,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components associated with this monitor.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *monitorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the monitor by ID from the API.
func (d *monitorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config monitorDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor data source: reading monitor", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetMonitor(ctx, id, &api.GetMonitorParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor", "Could not read monitor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Response", err.Error())
		return
	}

	// Re-use the flatten helper. monitorDataSourceModel is an alias of
	// monitorResourceModel so they are interchangeable here.
	var state monitorDataSourceModel
	resp.Diagnostics.Append(flattenMonitor(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

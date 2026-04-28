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
	_ datasource.DataSource              = &probeDataSource{}
	_ datasource.DataSourceWithConfigure = &probeDataSource{}
)

// NewProbeDataSource returns a new probeDataSource constructor function.
func NewProbeDataSource() datasource.DataSource {
	return &probeDataSource{}
}

// probeDataSource implements the data "monotaur_probe" data source.
type probeDataSource struct {
	client *client.Client
}

// probeDataSourceModel is an alias for probeResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type probeDataSourceModel = probeResourceModel

// Metadata sets the data source type name.
func (d *probeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_probe"
}

// Schema defines the Terraform schema for the data source.
func (d *probeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur probe by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the probe to look up.",
				Required:            true,
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the probe is active.",
				Computed:            true,
			},
			"schedule": schema.StringAttribute{
				MarkdownDescription: "The schedule expression for the probe.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the probe was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the probe was last updated.",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this probe belongs to.",
				Computed:            true,
			},
			"sensor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of sensors associated with this probe.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *probeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the probe by ID from the API.
func (d *probeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config probeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_probe data source: reading probe", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetProbe(ctx, id, &api.GetProbeParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe", "Could not read probe "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Probe", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInProbeResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe Response", err.Error())
		return
	}

	// Re-use the flatten helper. probeDataSourceModel is an alias of
	// probeResourceModel so they are interchangeable here.
	var state probeDataSourceModel
	resp.Diagnostics.Append(flattenProbe(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

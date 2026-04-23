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
	_ datasource.DataSource              = &sensorDataSource{}
	_ datasource.DataSourceWithConfigure = &sensorDataSource{}
)

// NewSensorDataSource returns a new sensorDataSource constructor function.
func NewSensorDataSource() datasource.DataSource {
	return &sensorDataSource{}
}

// sensorDataSource implements the data "monotaur_sensor" data source.
type sensorDataSource struct {
	client *client.Client
}

// sensorDataSourceModel is an alias for sensorResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type sensorDataSourceModel = sensorResourceModel

// Metadata sets the data source type name.
func (d *sensorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sensor"
}

// Schema defines the Terraform schema for the data source.
func (d *sensorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur sensor by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the sensor to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the sensor.",
				Computed:            true,
			},
			"plugin_name": schema.StringAttribute{
				MarkdownDescription: "The name of the plugin that powers this sensor.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The type identifier of the sensor.",
				Computed:            true,
			},
			"parameters": schema.StringAttribute{
				MarkdownDescription: "JSON-encoded parameters for the sensor plugin.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the sensor was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the sensor was last updated.",
				Computed:            true,
			},
			"probe_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the probe this sensor belongs to.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *sensorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the sensor by ID from the API.
func (d *sensorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config sensorDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_sensor data source: reading sensor", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetSensor(ctx, id, &api.GetSensorParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor", "Could not read sensor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSensorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor Response", err.Error())
		return
	}

	// Re-use the flatten helper. sensorDataSourceModel is an alias of
	// sensorResourceModel so they are interchangeable here.
	var state sensorDataSourceModel
	resp.Diagnostics.Append(flattenSensor(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

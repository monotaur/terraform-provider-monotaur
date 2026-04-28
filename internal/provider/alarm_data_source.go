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
	_ datasource.DataSource              = &alarmDataSource{}
	_ datasource.DataSourceWithConfigure = &alarmDataSource{}
)

// NewAlarmDataSource returns a new alarmDataSource constructor function.
func NewAlarmDataSource() datasource.DataSource {
	return &alarmDataSource{}
}

// alarmDataSource implements the data "monotaur_alarm" data source.
type alarmDataSource struct {
	client *client.Client
}

// alarmDataSourceModel is an alias for alarmResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type alarmDataSourceModel = alarmResourceModel

// Metadata sets the data source type name.
func (d *alarmDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alarm"
}

// Schema defines the Terraform schema for the data source.
func (d *alarmDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur alarm by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the alarm to look up.",
				Required:            true,
			},
			"start_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm started.",
				Computed:            true,
			},
			"end_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm ended.",
				Computed:            true,
			},
			"exclude_from_downtime": schema.BoolAttribute{
				MarkdownDescription: "Whether this alarm is excluded from downtime calculations.",
				Computed:            true,
			},
			"squelch": schema.BoolAttribute{
				MarkdownDescription: "Whether this alarm is squelched (suppressed from alerting).",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm was last updated.",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this alarm belongs to.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *alarmDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the alarm by ID from the API.
func (d *alarmDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config alarmDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm data source: reading alarm", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAlarm(ctx, id, &api.GetAlarmParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm", "Could not read alarm "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Response", err.Error())
		return
	}

	// Re-use the flatten helper. alarmDataSourceModel is an alias of
	// alarmResourceModel so they are interchangeable here.
	var state alarmDataSourceModel
	resp.Diagnostics.Append(flattenAlarm(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

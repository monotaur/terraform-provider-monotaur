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
	_ datasource.DataSource              = &businessHoursDataSource{}
	_ datasource.DataSourceWithConfigure = &businessHoursDataSource{}
)

// NewBusinessHoursDataSource returns a new businessHoursDataSource constructor function.
func NewBusinessHoursDataSource() datasource.DataSource {
	return &businessHoursDataSource{}
}

// businessHoursDataSource implements the data "monotaur_business_hours" data source.
type businessHoursDataSource struct {
	client *client.Client
}

// businessHoursDataSourceModel is an alias for businessHoursResourceModel; both
// share the same schema shape.
type businessHoursDataSourceModel = businessHoursResourceModel

// Metadata sets the data source type name.
func (d *businessHoursDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_business_hours"
}

// Schema defines the Terraform schema for the data source.
func (d *businessHoursDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur business-hours schedule by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the business hours to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the business hours.",
				Computed:            true,
			},
			"days_of_week": schema.SetAttribute{
				MarkdownDescription: "The days the time range applies to, as individual day names (`Sunday` … `Saturday`).",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"start_time": schema.StringAttribute{
				MarkdownDescription: "When the range starts each day, as a .NET TimeSpan (`hh:mm:ss`).",
				Computed:            true,
			},
			"end_time": schema.StringAttribute{
				MarkdownDescription: "When the range ends, as a .NET TimeSpan; earlier than `start_time` means it ends the following day, `1.00:00:00` means midnight.",
				Computed:            true,
			},
			"time_zone_id": schema.StringAttribute{
				MarkdownDescription: "The IANA or Windows time zone the times follow; null means UTC.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the business hours were created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the business hours were last updated.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *businessHoursDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the business hours by ID from the API.
func (d *businessHoursDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config businessHoursDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_business_hours data source: reading business hours", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetBusinessHour(ctx, id, &api.GetBusinessHourParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours", "Could not read business hours "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInBusinessHourResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours Response", err.Error())
		return
	}

	var state businessHoursDataSourceModel
	resp.Diagnostics.Append(flattenBusinessHours(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

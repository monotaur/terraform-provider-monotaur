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
	_ datasource.DataSource              = &maintenanceScheduleDataSource{}
	_ datasource.DataSourceWithConfigure = &maintenanceScheduleDataSource{}
)

// NewMaintenanceScheduleDataSource returns a new maintenanceScheduleDataSource constructor function.
func NewMaintenanceScheduleDataSource() datasource.DataSource {
	return &maintenanceScheduleDataSource{}
}

// maintenanceScheduleDataSource implements the data "monotaur_maintenance_schedule" data source.
type maintenanceScheduleDataSource struct {
	client *client.Client
}

// maintenanceScheduleDataSourceModel is an alias for maintenanceScheduleResourceModel;
// both share the same schema shape.
type maintenanceScheduleDataSourceModel = maintenanceScheduleResourceModel

// Metadata sets the data source type name.
func (d *maintenanceScheduleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance_schedule"
}

// Schema defines the Terraform schema for the data source.
func (d *maintenanceScheduleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur maintenance schedule by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the maintenance schedule to look up.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the maintenance, copied to every generated window.",
				Computed:            true,
			},
			"schedule": schema.StringAttribute{
				MarkdownDescription: "The cron expression for when each window starts.",
				Computed:            true,
			},
			"time_span": schema.StringAttribute{
				MarkdownDescription: "How long each window lasts, as a .NET TimeSpan `[d.]hh:mm:ss`.",
				Computed:            true,
			},
			"time_zone_id": schema.StringAttribute{
				MarkdownDescription: "The time zone `schedule` is evaluated in, or null for UTC.",
				Computed:            true,
			},
			"exclude_from_downtime": schema.BoolAttribute{
				MarkdownDescription: "Whether time inside the generated windows is excluded from downtime in uptime reports.",
				Computed:            true,
			},
			"apply_to_component_status": schema.BoolAttribute{
				MarkdownDescription: "Whether the components show as under maintenance while a generated window is active.",
				Computed:            true,
			},
			"squelch": schema.BoolAttribute{
				MarkdownDescription: "Whether alarm notifications for the components are suppressed while a generated window is active.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the schedule was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the schedule was last updated.",
				Computed:            true,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the components the schedule's windows apply to.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *maintenanceScheduleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the maintenance schedule by ID from the API.
func (d *maintenanceScheduleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config maintenanceScheduleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_maintenance_schedule data source: reading maintenance schedule", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetMaintenanceSchedule(ctx, id, maintenanceScheduleReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule", "Could not read maintenance schedule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMaintenanceScheduleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule Response", err.Error())
		return
	}

	var state maintenanceScheduleDataSourceModel
	resp.Diagnostics.Append(flattenMaintenanceSchedule(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

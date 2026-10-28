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
	_ datasource.DataSource              = &calendarEventDataSource{}
	_ datasource.DataSourceWithConfigure = &calendarEventDataSource{}
)

// NewCalendarEventDataSource returns a new calendarEventDataSource constructor function.
func NewCalendarEventDataSource() datasource.DataSource {
	return &calendarEventDataSource{}
}

// calendarEventDataSource implements the data "monotaur_calendar_event" data source.
type calendarEventDataSource struct {
	client *client.Client
}

// calendarEventDataSourceModel is an alias for calendarEventResourceModel;
// both share the same schema shape.
type calendarEventDataSourceModel = calendarEventResourceModel

// Metadata sets the data source type name.
func (d *calendarEventDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_calendar_event"
}

// Schema defines the Terraform schema for the data source.
func (d *calendarEventDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur calendar event by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the calendar event to look up.",
				Required:            true,
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The title of the event.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A longer description of the event, or null.",
				Computed:            true,
			},
			"start_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp (UTC) when the event starts.",
				Computed:            true,
			},
			"end_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp (UTC) when the event ends.",
				Computed:            true,
			},
			"external_reference": schema.StringAttribute{
				MarkdownDescription: "A reference to an external record such as a ticket or change request, or null.",
				Computed:            true,
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels attached to the event.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the event was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the event was last updated.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *calendarEventDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the calendar event by ID from the API.
func (d *calendarEventDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config calendarEventDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_calendar_event data source: reading calendar event", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetCalendarEvent(ctx, id, calendarEventReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event", "Could not read calendar event "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInCalendarEventResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event Response", err.Error())
		return
	}

	var state calendarEventDataSourceModel
	resp.Diagnostics.Append(flattenCalendarEvent(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

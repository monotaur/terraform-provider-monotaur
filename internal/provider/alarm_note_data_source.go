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
	_ datasource.DataSource              = &alarmNoteDataSource{}
	_ datasource.DataSourceWithConfigure = &alarmNoteDataSource{}
)

// NewAlarmNoteDataSource returns a new alarmNoteDataSource constructor function.
func NewAlarmNoteDataSource() datasource.DataSource {
	return &alarmNoteDataSource{}
}

// alarmNoteDataSource implements the data "monotaur_alarm_note" data source.
type alarmNoteDataSource struct {
	client *client.Client
}

// alarmNoteDataSourceModel is an alias for alarmNoteResourceModel; both share
// the same schema shape.
type alarmNoteDataSourceModel = alarmNoteResourceModel

// Metadata sets the data source type name.
func (d *alarmNoteDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alarm_note"
}

// Schema defines the Terraform schema for the data source.
func (d *alarmNoteDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur alarm note (an entry of an alarm's incident timeline) by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the note to look up.",
				Required:            true,
			},
			"alarm_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the alarm the note belongs to.",
				Computed:            true,
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "The stage of incident response the note reports: `Investigating`, `Mitigating` or `Resolved`.",
				Computed:            true,
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "The note text, in markdown.",
				Computed:            true,
			},
			"public": schema.BoolAttribute{
				MarkdownDescription: "Whether the public status page may show the note.",
				Computed:            true,
			},
			"author": schema.StringAttribute{
				MarkdownDescription: "Who wrote the note, e.g. `service-account:12`.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the note was created (its time on the timeline).",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the note was last updated.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *alarmNoteDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the note by ID from the API.
func (d *alarmNoteDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config alarmNoteDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm_note data source: reading alarm note", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAlarmNote(ctx, id, alarmNoteReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note", "Could not read alarm note "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmNoteResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note Response", err.Error())
		return
	}

	var state alarmNoteDataSourceModel
	flattenAlarmNote(data, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

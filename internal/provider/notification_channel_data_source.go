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
	_ datasource.DataSource              = &notificationChannelDataSource{}
	_ datasource.DataSourceWithConfigure = &notificationChannelDataSource{}
)

// NewNotificationChannelDataSource returns a new notificationChannelDataSource constructor function.
func NewNotificationChannelDataSource() datasource.DataSource {
	return &notificationChannelDataSource{}
}

// notificationChannelDataSource implements the data "monotaur_notification_channel" data source.
type notificationChannelDataSource struct {
	client *client.Client
}

// notificationChannelDataSourceModel mirrors notificationChannelResourceModel
// without the write-only credential, which the API never returns.
type notificationChannelDataSourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Type                    types.String `tfsdk:"type"`
	EventTypes              types.Set    `tfsdk:"event_types"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
	Recipients              types.List   `tfsdk:"recipients"`
	SubjectTemplate         types.String `tfsdk:"subject_template"`
	BodyTemplate            types.String `tfsdk:"body_template"`
	CreateDateTime          types.String `tfsdk:"create_date_time"`
	UpdateDateTime          types.String `tfsdk:"update_date_time"`
	ConsecutiveFailureCount types.Int64  `tfsdk:"consecutive_failure_count"`
	AutoDisabledDateTime    types.String `tfsdk:"auto_disabled_date_time"`
}

// Metadata sets the data source type name.
func (d *notificationChannelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_channel"
}

// Schema defines the Terraform schema for the data source.
func (d *notificationChannelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur notification channel by its ID. The channel's credential is write-only and not exposed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the notification channel to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the channel.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "What the channel delivers to: `Slack`, `Teams`, `Email`, `PagerDuty` or `Webhook`.",
				Computed:            true,
			},
			"event_types": schema.SetAttribute{
				MarkdownDescription: "The CloudEvent types delivered to the channel.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the channel sends notifications.",
				Computed:            true,
			},
			"recipients": schema.ListAttribute{
				MarkdownDescription: "Email addresses an `Email` channel sends to; empty for other types.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"subject_template": schema.StringAttribute{
				MarkdownDescription: "Scriban template of the subject, or null for the type's default.",
				Computed:            true,
			},
			"body_template": schema.StringAttribute{
				MarkdownDescription: "Scriban template of the message body, or null for the type's default.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the channel was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the channel was last updated.",
				Computed:            true,
			},
			"consecutive_failure_count": schema.Int64Attribute{
				MarkdownDescription: "Deliveries in a row that ended `Failed` (system-managed, read-only).",
				Computed:            true,
			},
			"auto_disabled_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when delivery disabled the channel after too many failures, or null.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *notificationChannelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the notification channel by ID from the API.
func (d *notificationChannelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config notificationChannelDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_channel data source: reading notification channel", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetNotificationChannel(ctx, id, &api.GetNotificationChannelParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel", "Could not read notification channel "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationChannelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Channel Response", err.Error())
		return
	}

	// Re-use the resource flatten helper through a temporary resource model.
	var m notificationChannelResourceModel
	resp.Diagnostics.Append(flattenNotificationChannel(ctx, data, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := notificationChannelDataSourceModel{
		ID:                      m.ID,
		Name:                    m.Name,
		Type:                    m.Type,
		EventTypes:              m.EventTypes,
		Enabled:                 m.Enabled,
		Recipients:              m.Recipients,
		SubjectTemplate:         m.SubjectTemplate,
		BodyTemplate:            m.BodyTemplate,
		CreateDateTime:          m.CreateDateTime,
		UpdateDateTime:          m.UpdateDateTime,
		ConsecutiveFailureCount: m.ConsecutiveFailureCount,
		AutoDisabledDateTime:    m.AutoDisabledDateTime,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

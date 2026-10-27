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
	_ datasource.DataSource              = &notificationPolicyDataSource{}
	_ datasource.DataSourceWithConfigure = &notificationPolicyDataSource{}
)

// NewNotificationPolicyDataSource returns a new notificationPolicyDataSource constructor function.
func NewNotificationPolicyDataSource() datasource.DataSource {
	return &notificationPolicyDataSource{}
}

// notificationPolicyDataSource implements the data "monotaur_notification_policy" data source.
type notificationPolicyDataSource struct {
	client *client.Client
}

// notificationPolicyDataSourceModel is an alias for notificationPolicyResourceModel;
// both share the same schema shape.
type notificationPolicyDataSourceModel = notificationPolicyResourceModel

// Metadata sets the data source type name.
func (d *notificationPolicyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_policy"
}

// Schema defines the Terraform schema for the data source.
func (d *notificationPolicyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur notification policy by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the notification policy to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the policy.",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy matches events.",
				Computed:            true,
			},
			"minimum_severity": schema.StringAttribute{
				MarkdownDescription: "The least severe alarm the policy matches (`Info`, `Warning`, `Error` or `Critical`), or null for any.",
				Computed:            true,
			},
			"repeat_interval_minutes": schema.Int64Attribute{
				MarkdownDescription: "How often, in minutes, an open and unacknowledged alarm is notified again, or null for no repeat.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the policy was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the policy was last updated.",
				Computed:            true,
			},
			"channel_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the notification channels the policy targets.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the labels the policy matches on.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the components the policy matches on.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the monitors the policy matches on.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *notificationPolicyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the notification policy by ID from the API.
func (d *notificationPolicyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config notificationPolicyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_policy data source: reading notification policy", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetNotificationPolicy(ctx, id, notificationPolicyReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy", "Could not read notification policy "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationPolicyResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Policy Response", err.Error())
		return
	}

	var state notificationPolicyDataSourceModel
	resp.Diagnostics.Append(flattenNotificationPolicy(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

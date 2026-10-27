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
	_ datasource.DataSource              = &notificationEscalationStepDataSource{}
	_ datasource.DataSourceWithConfigure = &notificationEscalationStepDataSource{}
)

// NewNotificationEscalationStepDataSource returns a new notificationEscalationStepDataSource constructor function.
func NewNotificationEscalationStepDataSource() datasource.DataSource {
	return &notificationEscalationStepDataSource{}
}

// notificationEscalationStepDataSource implements the data "monotaur_notification_escalation_step" data source.
type notificationEscalationStepDataSource struct {
	client *client.Client
}

// notificationEscalationStepDataSourceModel is an alias for
// notificationEscalationStepResourceModel; both share the same schema shape.
type notificationEscalationStepDataSourceModel = notificationEscalationStepResourceModel

// Metadata sets the data source type name.
func (d *notificationEscalationStepDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_escalation_step"
}

// Schema defines the Terraform schema for the data source.
func (d *notificationEscalationStepDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur notification escalation step by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the escalation step to look up.",
				Required:            true,
			},
			"notification_policy_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the notification policy the step belongs to.",
				Computed:            true,
			},
			"delay_minutes": schema.Int64Attribute{
				MarkdownDescription: "Minutes after the alarm was reported that the step fires.",
				Computed:            true,
			},
			"channel_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the notification channels the step notifies.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the step was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the step was last updated.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *notificationEscalationStepDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the escalation step by ID from the API.
func (d *notificationEscalationStepDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config notificationEscalationStepDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_notification_escalation_step data source: reading escalation step", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetNotificationEscalationStep(ctx, id, notificationEscalationStepReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step", "Could not read escalation step "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInNotificationEscalationStepResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Notification Escalation Step Response", err.Error())
		return
	}

	var state notificationEscalationStepDataSourceModel
	resp.Diagnostics.Append(flattenNotificationEscalationStep(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

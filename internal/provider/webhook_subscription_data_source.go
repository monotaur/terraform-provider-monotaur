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
	_ datasource.DataSource              = &webhookSubscriptionDataSource{}
	_ datasource.DataSourceWithConfigure = &webhookSubscriptionDataSource{}
)

// NewWebhookSubscriptionDataSource returns a new webhookSubscriptionDataSource constructor function.
func NewWebhookSubscriptionDataSource() datasource.DataSource {
	return &webhookSubscriptionDataSource{}
}

// webhookSubscriptionDataSource implements the data "monotaur_webhook_subscription" data source.
type webhookSubscriptionDataSource struct {
	client *client.Client
}

// webhookSubscriptionDataSourceModel mirrors webhookSubscriptionResourceModel
// without the signing keys, which the API never returns.
type webhookSubscriptionDataSourceModel struct {
	ID                               types.String `tfsdk:"id"`
	URL                              types.String `tfsdk:"url"`
	EventTypes                       types.Set    `tfsdk:"event_types"`
	PayloadMode                      types.String `tfsdk:"payload_mode"`
	Enabled                          types.Bool   `tfsdk:"enabled"`
	LegacySignatureEnabled           types.Bool   `tfsdk:"legacy_signature_enabled"`
	CreateDateTime                   types.String `tfsdk:"create_date_time"`
	UpdateDateTime                   types.String `tfsdk:"update_date_time"`
	PreviousSigningKeyExpiryDateTime types.String `tfsdk:"previous_signing_key_expiry_date_time"`
	ConsecutiveFailureCount          types.Int64  `tfsdk:"consecutive_failure_count"`
	AutoDisabledDateTime             types.String `tfsdk:"auto_disabled_date_time"`
}

// Metadata sets the data source type name.
func (d *webhookSubscriptionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_subscription"
}

// Schema defines the Terraform schema for the data source.
func (d *webhookSubscriptionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur webhook subscription by its ID. The signing key is write-only and not exposed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the webhook subscription to look up.",
				Required:            true,
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "The `https` URL deliveries are POSTed to.",
				Computed:            true,
			},
			"event_types": schema.SetAttribute{
				MarkdownDescription: "The CloudEvent types delivered to the subscription.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"payload_mode": schema.StringAttribute{
				MarkdownDescription: "How much of the resource a delivery carries: `Full` or `ChangesOnly`.",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether events are delivered to the subscription.",
				Computed:            true,
			},
			"legacy_signature_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether deliveries also carry the deprecated body-only `X-Monotaur-Signature-256` header.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the subscription was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the subscription was last updated.",
				Computed:            true,
			},
			"previous_signing_key_expiry_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp until which deliveries are also signed with the previous key, or null.",
				Computed:            true,
			},
			"consecutive_failure_count": schema.Int64Attribute{
				MarkdownDescription: "Deliveries in a row that ended `Failed` (system-managed, read-only).",
				Computed:            true,
			},
			"auto_disabled_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when delivery disabled the subscription after too many failures, or null.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *webhookSubscriptionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the webhook subscription by ID from the API.
func (d *webhookSubscriptionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config webhookSubscriptionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_webhook_subscription data source: reading webhook subscription", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetWebhookSubscription(ctx, id, &api.GetWebhookSubscriptionParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription", "Could not read webhook subscription "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInWebhookSubscriptionResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook Subscription Response", err.Error())
		return
	}

	// Re-use the resource flatten helper through a temporary resource model.
	var m webhookSubscriptionResourceModel
	resp.Diagnostics.Append(flattenWebhookSubscription(ctx, data, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := webhookSubscriptionDataSourceModel{
		ID:                               m.ID,
		URL:                              m.URL,
		EventTypes:                       m.EventTypes,
		PayloadMode:                      m.PayloadMode,
		Enabled:                          m.Enabled,
		LegacySignatureEnabled:           m.LegacySignatureEnabled,
		CreateDateTime:                   m.CreateDateTime,
		UpdateDateTime:                   m.UpdateDateTime,
		PreviousSigningKeyExpiryDateTime: m.PreviousSigningKeyExpiryDateTime,
		ConsecutiveFailureCount:          m.ConsecutiveFailureCount,
		AutoDisabledDateTime:             m.AutoDisabledDateTime,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

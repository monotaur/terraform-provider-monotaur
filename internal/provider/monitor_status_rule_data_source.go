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
	_ datasource.DataSource              = &monitorStatusRuleDataSource{}
	_ datasource.DataSourceWithConfigure = &monitorStatusRuleDataSource{}
)

// NewMonitorStatusRuleDataSource returns a new monitorStatusRuleDataSource constructor function.
func NewMonitorStatusRuleDataSource() datasource.DataSource {
	return &monitorStatusRuleDataSource{}
}

// monitorStatusRuleDataSource implements the data "monotaur_monitor_status_rule" data source.
type monitorStatusRuleDataSource struct {
	client *client.Client
}

// monitorStatusRuleDataSourceModel is an alias for monitorStatusRuleResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type monitorStatusRuleDataSourceModel = monitorStatusRuleResourceModel

// Metadata sets the data source type name.
func (d *monitorStatusRuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_status_rule"
}

// Schema defines the Terraform schema for the data source.
func (d *monitorStatusRuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur monitor status rule by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the monitor status rule to look up.",
				Required:            true,
			},
			"predicate": schema.StringAttribute{
				MarkdownDescription: "The predicate expression that determines when the rule applies.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The monitor status to apply when the predicate matches.",
				Computed:            true,
			},
			"status_message": schema.StringAttribute{
				MarkdownDescription: "A message to accompany the status when the predicate matches.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor status rule was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor status rule was last updated.",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this status rule belongs to.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *monitorStatusRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the monitor status rule by ID from the API.
func (d *monitorStatusRuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config monitorStatusRuleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor_status_rule data source: reading monitor status rule", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetMonitorStatusRule(ctx, id, &api.GetMonitorStatusRuleParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule", "Could not read monitor status rule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorStatusRuleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Status Rule Response", err.Error())
		return
	}

	// Re-use the flatten helper. monitorStatusRuleDataSourceModel is an alias of
	// monitorStatusRuleResourceModel so they are interchangeable here.
	var state monitorStatusRuleDataSourceModel
	resp.Diagnostics.Append(flattenMonitorStatusRule(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

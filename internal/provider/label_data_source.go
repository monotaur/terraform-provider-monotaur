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
	_ datasource.DataSource              = &labelDataSource{}
	_ datasource.DataSourceWithConfigure = &labelDataSource{}
)

// NewLabelDataSource returns a new labelDataSource constructor function.
func NewLabelDataSource() datasource.DataSource {
	return &labelDataSource{}
}

// labelDataSource implements the data "monotaur_label" data source.
type labelDataSource struct {
	client *client.Client
}

// labelDataSourceModel is an alias for labelResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type labelDataSourceModel = labelResourceModel

// Metadata sets the data source type name.
func (d *labelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label"
}

// Schema defines the Terraform schema for the data source.
func (d *labelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur label by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the label to look up.",
				Required:            true,
			},
			"text": schema.StringAttribute{
				MarkdownDescription: "The display text of the label.",
				Computed:            true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "Hex colour code for the label (e.g. `#FF5733`).",
				Computed:            true,
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Icon identifier for the label.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The machine-readable name of the label.",
				Computed:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value of the label.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the label was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the label was last updated.",
				Computed:            true,
			},
			"calendar_event_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of calendar events associated with this label.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components associated with this label.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of monitors associated with this label.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *labelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the label by ID from the API.
func (d *labelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config labelDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_label data source: reading label", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetLabel(ctx, id, &api.GetLabelParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label", "Could not read label "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Label", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInLabelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label Response", err.Error())
		return
	}

	// Re-use the flatten helper. labelDataSourceModel is an alias of
	// labelResourceModel so they are interchangeable here.
	var state labelDataSourceModel
	resp.Diagnostics.Append(flattenLabel(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

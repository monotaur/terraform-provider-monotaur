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
	_ datasource.DataSource              = &secretDataSource{}
	_ datasource.DataSourceWithConfigure = &secretDataSource{}
)

// NewSecretDataSource returns a new secretDataSource constructor function.
func NewSecretDataSource() datasource.DataSource {
	return &secretDataSource{}
}

// secretDataSource implements the data "monotaur_secret" data source.
type secretDataSource struct {
	client *client.Client
}

// secretDataSourceModel is an alias for secretResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type secretDataSourceModel = secretResourceModel

// Metadata sets the data source type name.
func (d *secretDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

// Schema defines the Terraform schema for the data source.
func (d *secretDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur secret by its ID. Note: the `value` attribute will be empty because the API does not return secret values on read.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the secret to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the secret.",
				Computed:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The secret value. Always empty in the data source because the API does not return secret values on read.",
				Computed:            true,
				Sensitive:           true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the secret.",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the secret was created.",
				Computed:            true,
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the secret was last updated.",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this secret belongs to.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *secretDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the secret by ID from the API.
func (d *secretDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config secretDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_secret data source: reading secret", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetSecret(ctx, id, &api.GetSecretParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Secret", "Could not read secret "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Secret", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSecretResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Secret Response", err.Error())
		return
	}

	// Re-use the flatten helper. secretDataSourceModel is an alias of
	// secretResourceModel so they are interchangeable here.
	var state secretDataSourceModel
	resp.Diagnostics.Append(flattenSecret(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

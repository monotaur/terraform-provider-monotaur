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
	_ datasource.DataSource              = &apiKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &apiKeyDataSource{}
)

// NewApiKeyDataSource returns a new apiKeyDataSource constructor function.
func NewApiKeyDataSource() datasource.DataSource {
	return &apiKeyDataSource{}
}

// apiKeyDataSource implements the data "monotaur_api_key" data source.
type apiKeyDataSource struct {
	client *client.Client
}

// apiKeyDataSourceModel is an alias for apiKeyResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
//
// Note: the key_value attribute will always be empty in the data source because
// the API does not return the API key value on read. The key value is only
// available immediately after creation.
type apiKeyDataSourceModel = apiKeyResourceModel

// Metadata sets the data source type name.
func (d *apiKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

// Schema defines the Terraform schema for the data source.
func (d *apiKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur API key by its ID. " +
			"Note: the `key_value` attribute will always be empty because the API does not return the key value on read. " +
			"The key value is only available immediately after the `monotaur_api_key` resource is created.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the API key to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the API key.",
				Computed:            true,
			},
			"environment": schema.StringAttribute{
				MarkdownDescription: "The environment this API key is associated with.",
				Computed:            true,
			},
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the service account that owns this API key.",
				Computed:            true,
			},
			"key_value": schema.StringAttribute{
				MarkdownDescription: "Always empty in the data source — the API does not return the key value on read.",
				Computed:            true,
				Sensitive:           true,
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "A non-sensitive prefix of the API key value, useful for identification.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was created.",
				Computed:            true,
			},
			"created_by": schema.StringAttribute{
				MarkdownDescription: "The identity that created this API key.",
				Computed:            true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key expires, if set.",
				Computed:            true,
			},
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was last used.",
				Computed:            true,
			},
			"revoked_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was revoked, if applicable.",
				Computed:            true,
			},
			"revoked_by": schema.StringAttribute{
				MarkdownDescription: "The identity that revoked this API key, if applicable.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *apiKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the API key by ID from the API.
func (d *apiKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config apiKeyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_api_key data source: reading API key", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAdminApiKey(ctx, id, &api.GetAdminApiKeyParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading API Key", "Could not read API key "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading API Key", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminApiKeyResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading API Key Response", err.Error())
		return
	}

	// Re-use the flatten helper. apiKeyDataSourceModel is an alias of
	// apiKeyResourceModel so they are interchangeable here.
	// key_value will remain empty (zero value) since flattenApiKey intentionally
	// does not set it — the API does not return the key value on read.
	var state apiKeyDataSourceModel
	resp.Diagnostics.Append(flattenApiKey(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

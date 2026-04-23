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
	_ datasource.DataSource              = &serviceAccountDataSource{}
	_ datasource.DataSourceWithConfigure = &serviceAccountDataSource{}
)

// NewServiceAccountDataSource returns a new serviceAccountDataSource constructor function.
func NewServiceAccountDataSource() datasource.DataSource {
	return &serviceAccountDataSource{}
}

// serviceAccountDataSource implements the data "monotaur_service_account" data source.
type serviceAccountDataSource struct {
	client *client.Client
}

// serviceAccountDataSourceModel is an alias for serviceAccountResourceModel. Both structs share
// the same schema shape, so a type alias removes duplication without any
// runtime cost.
type serviceAccountDataSourceModel = serviceAccountResourceModel

// Metadata sets the data source type name.
func (d *serviceAccountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

// Schema defines the Terraform schema for the data source.
func (d *serviceAccountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Monotaur service account by its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the service account to look up.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the service account.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the service account.",
				Computed:            true,
			},
			"disabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the service account is disabled.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the service account was created.",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (d *serviceAccountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the service account by ID from the API.
func (d *serviceAccountDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config serviceAccountDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "monotaur_service_account data source: reading service account", map[string]any{"id": id})

	apiResp, err := d.client.Inner().GetAdminServiceAccount(ctx, id, &api.GetAdminServiceAccountParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account", "Could not read service account "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminServiceAccountResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Service Account Response", err.Error())
		return
	}

	// Re-use the flatten helper. serviceAccountDataSourceModel is an alias of
	// serviceAccountResourceModel so they are interchangeable here.
	var state serviceAccountDataSourceModel
	resp.Diagnostics.Append(flattenServiceAccount(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

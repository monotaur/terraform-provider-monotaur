package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure MonotaurProvider satisfies the provider.Provider interface.
var _ provider.Provider = &MonotaurProvider{}

// MonotaurProvider defines the provider implementation.
type MonotaurProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and run locally, and "test" when running acceptance
	// tests.
	version string
}

// MonotaurProviderModel describes the provider data model.
type MonotaurProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

// New returns a provider constructor function.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MonotaurProvider{
			version: version,
		}
	}
}

// Metadata returns the provider type name and version.
func (p *MonotaurProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "monotaur"
	resp.Version = p.version
}

// Schema defines the provider-level configuration schema.
func (p *MonotaurProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Monotaur provider manages resources via the Monotaur API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "The base URL of the Monotaur API. Can also be set via the `MONOTAUR_ENDPOINT` environment variable.",
				Optional:            true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "The API key used to authenticate with the Monotaur API. Can also be set via the `MONOTAUR_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

// Configure prepares a Monotaur API client for data sources and resources.
func (p *MonotaurProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config MonotaurProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve endpoint: config takes precedence, then env var.
	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if !config.Endpoint.IsNull() && !config.Endpoint.IsUnknown() {
		endpoint = config.Endpoint.ValueString()
	}

	// Resolve api_key: config takes precedence, then env var.
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if !config.APIKey.IsNull() && !config.APIKey.IsUnknown() {
		apiKey = config.APIKey.ValueString()
	}

	// Store client configuration values on the provider data so resources and
	// data sources can retrieve them via their Configure methods. In a future
	// issue an actual HTTP client will be constructed here.
	_ = endpoint
	_ = apiKey
}

// Resources returns the list of resources implemented by this provider.
func (p *MonotaurProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

// DataSources returns the list of data sources implemented by this provider.
func (p *MonotaurProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

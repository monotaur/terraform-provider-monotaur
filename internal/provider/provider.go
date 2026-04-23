package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
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
//
// Configuration precedence:
//  1. Explicit value in the provider block
//  2. Environment variable (MONOTAUR_API_KEY / MONOTAUR_ENDPOINT)
//  3. Error — api_key is required; endpoint defaults to the Monotaur cloud URL
//     when neither source provides a value.
//
// The constructed *client.Client is passed to resources and data sources via
// resp.ResourceData so they can retrieve it in their own Configure methods.
func (p *MonotaurProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config MonotaurProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve endpoint: explicit provider block value takes precedence, then env var.
	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if !config.Endpoint.IsNull() && !config.Endpoint.IsUnknown() {
		endpoint = config.Endpoint.ValueString()
	}
	if endpoint == "" {
		endpoint = "https://api.monotaur.io"
	}

	// Resolve api_key: explicit provider block value takes precedence, then env var.
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if !config.APIKey.IsNull() && !config.APIKey.IsUnknown() {
		apiKey = config.APIKey.ValueString()
	}

	// api_key is required — return an actionable error when it is absent.
	if apiKey == "" {
		resp.Diagnostics.Append(diag.NewErrorDiagnostic(
			"Missing API Key",
			"The Monotaur provider requires an API key. "+
				"Set the `api_key` attribute in the provider block or the `MONOTAUR_API_KEY` environment variable.",
		))
		return
	}

	// Log the resolved configuration, redacting the api_key value.
	tflog.Debug(ctx, "monotaur: provider configured", map[string]any{
		"endpoint":    endpoint,
		"api_key_set": true,
	})

	c, err := client.New(client.Config{
		BaseURL: endpoint,
		APIKey:  apiKey,
	})
	if err != nil {
		resp.Diagnostics.Append(diag.NewErrorDiagnostic(
			"Failed to Create Monotaur Client",
			"An unexpected error occurred while creating the Monotaur API client: "+err.Error(),
		))
		return
	}

	// Pass the configured client to all resources and data sources.
	resp.ResourceData = c
	resp.DataSourceData = c
}

// Resources returns the list of resources implemented by this provider.
func (p *MonotaurProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewLabelResource,
		NewComponentResource,
		NewMonitorResource,
		NewMonitorStatusRuleResource,
	}
}

// DataSources returns the list of data sources implemented by this provider.
func (p *MonotaurProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewLabelDataSource,
		NewComponentDataSource,
		NewMonitorDataSource,
		NewMonitorStatusRuleDataSource,
	}
}

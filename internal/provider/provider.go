package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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

	// Unknown values occur when an attribute is derived from another resource
	// that has not yet been applied. Configuring the provider at plan time with
	// unknown inputs is not supported — emit a framework-standard diagnostic so
	// Terraform can surface a clear error rather than silently falling back to
	// environment variables.
	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown Endpoint",
			"The Monotaur provider cannot be configured with an unknown endpoint value. "+
				"Resolve the upstream resource before referencing its output here, or set "+
				"the endpoint explicitly in the provider block.",
		)
	}
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown API Key",
			"The Monotaur provider cannot be configured with an unknown api_key value. "+
				"Resolve the upstream resource before referencing its output here, or set "+
				"the api_key explicitly in the provider block.",
		)
	}
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
		tflog.Info(ctx, "monotaur: endpoint not set, defaulting to Monotaur cloud API", map[string]any{
			"endpoint": endpoint,
		})
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

	// Register the raw api_key string as a masked value so tflog scrubs it from
	// ALL subsequent log output — including free-form message strings and field
	// values. This must happen before any Debug/Info calls that could reference
	// the resolved configuration, so the key is never written to the log in plain
	// text. tflog.MaskLogStrings redacts the literal string wherever it appears,
	// unlike MaskFieldValuesWithFieldKeys which only redacts entries whose field
	// *key* is "api_key".
	ctx = tflog.MaskLogStrings(ctx, apiKey)

	// Log the resolved configuration. The api_key value is already masked in ctx
	// so even if it were included here it would be scrubbed before emission.
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
		NewAlarmResource,
		NewLabelResource,
		NewComponentResource,
		NewMonitorResource,
		NewMonitorStatusRuleResource,
		NewProbeResource,
		NewRoleResource,
		NewSecretResource,
		NewSensorResource,
		NewVariableResource,
	}
}

// DataSources returns the list of data sources implemented by this provider.
func (p *MonotaurProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAlarmDataSource,
		NewLabelDataSource,
		NewComponentDataSource,
		NewMonitorDataSource,
		NewMonitorStatusRuleDataSource,
		NewProbeDataSource,
		NewRoleDataSource,
		NewSecretDataSource,
		NewSensorDataSource,
		NewVariableDataSource,
	}
}

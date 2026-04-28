package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &apiKeyResource{}
	_ resource.ResourceWithConfigure   = &apiKeyResource{}
	_ resource.ResourceWithImportState = &apiKeyResource{}
)

// NewApiKeyResource returns a new apiKeyResource constructor function.
func NewApiKeyResource() resource.Resource {
	return &apiKeyResource{}
}

// apiKeyResource implements the monotaur_api_key managed resource.
type apiKeyResource struct {
	client *client.Client
}

// apiKeyResourceModel is the Terraform state model for an API key.
//
// Attribute mapping:
//
//	AttributesInCreateAdminApiKeyRequest: name (required), environment (required), expires_at (optional)
//	AttributesInAdminApiKeyResponse:      id (computed), name, environment, created_at (computed),
//	                                       created_by (computed), prefix (computed), last_used_at (computed),
//	                                       revoked_at (computed), revoked_by (computed), plaintext (write-once)
//	RelationshipsInCreateAdminApiKeyRequest: service_account_id (required)
//
// Note: the API only returns the `plaintext` (key value) field in the create response.
// Subsequent GET requests omit it. UseStateForUnknown on `key_value` preserves the
// value from the create response in Terraform state even when the GET response omits it.
type apiKeyResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Environment      types.String `tfsdk:"environment"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	KeyValue         types.String `tfsdk:"key_value"`
	Prefix           types.String `tfsdk:"prefix"`
	CreatedAt        types.String `tfsdk:"created_at"`
	CreatedBy        types.String `tfsdk:"created_by"`
	ExpiresAt        types.String `tfsdk:"expires_at"`
	LastUsedAt       types.String `tfsdk:"last_used_at"`
	RevokedAt        types.String `tfsdk:"revoked_at"`
	RevokedBy        types.String `tfsdk:"revoked_by"`
}

// Metadata sets the resource type name.
func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

// Schema defines the Terraform schema for the resource.
func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur API key for a service account. " +
			"API keys are write-once credentials: the key value is only returned by the API at creation time. " +
			"Lost key values cannot be recovered — create a new key and delete the old one to rotate.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the API key (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the API key.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"environment": schema.StringAttribute{
				MarkdownDescription: "The environment this API key is associated with.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the service account that owns this API key.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key_value": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "The API key value. Only available immediately after creation — not returned by the API on subsequent reads. " +
					"Once lost, the key cannot be recovered; create a new key and delete this one to rotate. " +
					"After import, this attribute will be empty — the key value is not recoverable.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "A non-sensitive prefix of the API key value, useful for identification.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by": schema.StringAttribute{
				MarkdownDescription: "The identity that created this API key (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key expires (optional). Changing this value requires replacement.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was last used (assigned by the API, updated on use).",
				Computed:            true,
			},
			"revoked_at": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the API key was revoked, if applicable (assigned by the API).",
				Computed:            true,
			},
			"revoked_by": schema.StringAttribute{
				MarkdownDescription: "The identity that revoked this API key, if applicable (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = c
}

// Create creates a new API key via POST /admin.apiKeys.
func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateAdminApiKeyRequest{
		OpenapiDiscriminator: api.ResourceTypeAdminApiKeys,
		Name:                 plan.Name.ValueString(),
		Environment:          plan.Environment.ValueString(),
	}

	if !plan.ExpiresAt.IsNull() && !plan.ExpiresAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.ExpiresAt.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid expires_at Value",
				fmt.Sprintf("Could not parse expires_at as RFC3339 timestamp: %s", err.Error()),
			)
			return
		}
		attrs.ExpiresAt = &t
	}

	body := api.CreateAdminApiKeyRequestDocument{
		Data: api.DataInCreateAdminApiKeyRequest{
			Type:       api.ResourceTypeAdminApiKeys,
			Attributes: attrs,
			Relationships: &api.RelationshipsInCreateAdminApiKeyRequest{
				ServiceAccount: api.ToOneAdminServiceAccountInRequest{
					Data: api.AdminServiceAccountIdentifierInRequest{
						Id:   plan.ServiceAccountID.ValueString(),
						Type: api.ResourceTypeAdminServiceAccounts,
					},
				},
			},
		},
	}

	tflog.Debug(ctx, "monotaur_api_key: creating API key", map[string]any{
		"name":               plan.Name.ValueString(),
		"environment":        plan.Environment.ValueString(),
		"service_account_id": plan.ServiceAccountID.ValueString(),
	})

	apiResp, err := r.client.Inner().PostAdminApiKeyWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAdminApiKeyParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating API Key", "Could not create API key: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating API Key", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAdminApiKeyResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading API Key Response", err.Error())
		return
	}

	// Capture key_value from the create response BEFORE calling flattenApiKey.
	// The API returns plaintext only on creation; flattenApiKey intentionally
	// omits it so subsequent reads preserve the state value via UseStateForUnknown.
	if data.Attributes != nil && data.Attributes.Plaintext != nil {
		plan.KeyValue = types.StringValue(*data.Attributes.Plaintext)
	}

	resp.Diagnostics.Append(flattenApiKey(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_api_key: created API key", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_api_key: reading API key", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetAdminApiKey(ctx, id, &api.GetAdminApiKeyParams{})
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

	// Pass the existing state so flattenApiKey preserves the write-once key_value
	// field when the API omits it from the GET response.
	resp.Diagnostics.Append(flattenApiKey(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is not supported for API keys. All mutable attributes are marked
// RequiresReplace, so any change triggers a replacement.
// This method satisfies the resource.Resource interface but should never be called
// in practice.
func (r *apiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"API Key Cannot Be Updated",
		"API keys are immutable. Changing any attribute requires destroying and recreating the resource.",
	)
}

// Delete removes an API key by removing it from the owning service account's
// relationship. The API does not expose a direct DELETE /admin.apiKeys/{id}
// endpoint; instead, keys are deleted via the service account relationship endpoint.
func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	serviceAccountID := state.ServiceAccountID.ValueString()
	tflog.Debug(ctx, "monotaur_api_key: deleting API key", map[string]any{
		"id":                 id,
		"service_account_id": serviceAccountID,
	})

	body := api.ToManyAdminApiKeyInRequest{
		Data: []api.AdminApiKeyIdentifierInRequest{
			{
				Id:   id,
				Type: api.ResourceTypeAdminApiKeys,
			},
		},
	}

	apiResp, err := r.client.Inner().DeleteAdminServiceAccountApiKeysRelationshipWithApplicationVndAPIPlusJSONExtOpenapiBody(
		ctx,
		serviceAccountID,
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting API Key", "Could not delete API key "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting API Key", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_api_key: deleted API key", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_api_key.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
//
// Note: the key_value attribute will be empty after import because the API does
// not return the key value on subsequent reads. The key value is only available
// immediately after creation and cannot be recovered. To rotate a key, create a
// new monotaur_api_key resource and delete this one.
func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenApiKey maps the API DataInAdminApiKeyResponse onto the Terraform state model.
//
// Write-once key_value handling: the API only returns the plaintext (key value)
// field in the create response. On subsequent GET requests, plaintext is omitted.
// flattenApiKey intentionally does NOT set model.KeyValue; the model's existing
// KeyValue field is left unchanged so Terraform state retains the value set at
// create time (via UseStateForUnknown preserving it across plans and reads).
//
// Callers that need to capture the key_value from a create response must set
// model.KeyValue BEFORE calling flattenApiKey.
func flattenApiKey(_ context.Context, data api.DataInAdminApiKeyResponse, model *apiKeyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Environment != nil {
			model.Environment = types.StringValue(*attrs.Environment)
		} else {
			model.Environment = types.StringNull()
		}

		if attrs.Prefix != nil {
			model.Prefix = types.StringValue(*attrs.Prefix)
		} else {
			model.Prefix = types.StringNull()
		}

		if attrs.CreatedAt != nil {
			model.CreatedAt = types.StringValue(attrs.CreatedAt.Format(time.RFC3339))
		} else {
			model.CreatedAt = types.StringNull()
		}

		if attrs.CreatedBy != nil {
			model.CreatedBy = types.StringValue(*attrs.CreatedBy)
		} else {
			model.CreatedBy = types.StringNull()
		}

		if attrs.ExpiresAt != nil {
			model.ExpiresAt = types.StringValue(attrs.ExpiresAt.Format(time.RFC3339))
		} else {
			model.ExpiresAt = types.StringNull()
		}

		if attrs.LastUsedAt != nil {
			model.LastUsedAt = types.StringValue(attrs.LastUsedAt.Format(time.RFC3339))
		} else {
			model.LastUsedAt = types.StringNull()
		}

		if attrs.RevokedAt != nil {
			model.RevokedAt = types.StringValue(attrs.RevokedAt.Format(time.RFC3339))
		} else {
			model.RevokedAt = types.StringNull()
		}

		if attrs.RevokedBy != nil {
			model.RevokedBy = types.StringValue(*attrs.RevokedBy)
		} else {
			model.RevokedBy = types.StringNull()
		}

		// `key_value` (plaintext) is intentionally NOT set here. The API only
		// returns it on the create response. The model already holds the key_value
		// set before this call (from the create response or prior state), so
		// leaving the field unchanged preserves the correct value in Terraform state
		// without triggering spurious drift detection.
	}

	if data.Relationships != nil {
		rels := data.Relationships

		// service_account_id — to-one
		if rels.ServiceAccount != nil && rels.ServiceAccount.Data != nil {
			model.ServiceAccountID = types.StringValue(rels.ServiceAccount.Data.Id)
		} else {
			model.ServiceAccountID = types.StringNull()
		}
	} else {
		model.ServiceAccountID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// ApiKeyResourceModelForTest is a type alias for apiKeyResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type ApiKeyResourceModelForTest = apiKeyResourceModel

// FlattenApiKeyForTest exposes flattenApiKey for use in unit tests.
func FlattenApiKeyForTest(ctx context.Context, data api.DataInAdminApiKeyResponse, model *apiKeyResourceModel) diag.Diagnostics {
	return flattenApiKey(ctx, data, model)
}

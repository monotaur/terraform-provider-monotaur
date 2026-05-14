package provider

import (
	"context"
	"fmt"
	"net/http"
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
	_ resource.Resource                = &secretResource{}
	_ resource.ResourceWithConfigure   = &secretResource{}
	_ resource.ResourceWithImportState = &secretResource{}
)

// NewSecretResource returns a new secretResource constructor function.
func NewSecretResource() resource.Resource {
	return &secretResource{}
}

// secretResource implements the monotaur_secret managed resource.
type secretResource struct {
	client *client.Client
}

// secretResourceModel is the Terraform state model for a secret.
//
// Attribute mapping:
//
//	AttributesInCreateSecretRequest:  name (required), value (required), description (optional)
//	AttributesInSecretResponse:       id (computed), name, description,
//	                                   createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*SecretRequest:    monitor_id (to-one, optional/nullable)
//
// Note: the API never returns the `value` field on read (write-only secret).
// UseStateForUnknown on `value` preserves the value set at create/update time
// in Terraform state even when the GET response omits it.
type secretResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
	MonitorID      types.String `tfsdk:"monitor_id"`
}

// Metadata sets the resource type name.
func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

// Schema defines the Terraform schema for the resource.
func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur secret. Secrets are named key-value pairs (with a sensitive value) that can optionally be associated with a monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the secret (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the secret.",
				Required:            true,
			},
			"value": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				MarkdownDescription: "The secret value. Sensitive and write-only: not returned by the API on read; " +
					"the value set at create/update time is retained in Terraform state.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "An optional description of the secret.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the secret was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the secret was last updated (assigned by the API).",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this secret belongs to (optional).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new secret via POST /secrets.
func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateSecretRequest{
		OpenapiDiscriminator: api.ResourceTypeSecrets,
		Name:                 plan.Name.ValueString(),
		Value:                plan.Value.ValueString(),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	rels, diags := buildCreateSecretRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateSecretRequestDocument{
		Data: api.DataInCreateSecretRequest{
			Type:          api.ResourceTypeSecrets,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_secret: creating secret", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostSecretWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostSecretParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Secret", "Could not create secret: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Secret", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSecretResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Secret Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenSecret(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_secret: created secret", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_secret: reading secret", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetSecret(ctx, id, &api.GetSecretParams{})
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

	// Pass the existing state so flattenSecret can preserve the write-only value
	// field when the API omits it from the response.
	resp.Diagnostics.Append(flattenSecret(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing secret via PATCH /secrets/{id}.
func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateSecretRequest{
		OpenapiDiscriminator: api.ResourceTypeSecrets,
	}

	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		attrs.Name = &v
	}

	if !plan.Value.IsNull() && !plan.Value.IsUnknown() {
		v := plan.Value.ValueString()
		attrs.Value = &v
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		attrs.Description = &v
	}

	rels, diags := buildUpdateSecretRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateSecretRequestDocument{
		Data: api.DataInUpdateSecretRequest{
			Id:            id,
			Type:          api.ResourceTypeSecrets,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_secret: updating secret", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchSecretWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchSecretParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Secret", "Could not update secret "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Secret", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetSecret(ctx, id, &api.GetSecretParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Secret After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInSecretResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Secret Response", err.Error())
		return
	}

	// Pass plan so flattenSecret preserves the write-only value from the plan
	// when the API omits it from the PATCH response.
	resp.Diagnostics.Append(flattenSecret(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_secret: updated secret", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a secret via DELETE /secrets/{id}.
func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_secret: deleting secret", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteSecret(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Secret", "Could not delete secret "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Secret", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_secret: deleted secret", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_secret.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateSecretRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateSecretRelationships(_ context.Context, plan secretResourceModel) (*api.RelationshipsInCreateSecretRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateSecretRequest{
		OpenapiDiscriminator: api.ResourceTypeSecrets,
	}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.NullableToOneMonitorInRequest{
			Data: &api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		}
	}

	return rels, diags
}

// buildUpdateSecretRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateSecretRelationships(_ context.Context, plan secretResourceModel) (*api.RelationshipsInUpdateSecretRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInUpdateSecretRequest{
		OpenapiDiscriminator: api.ResourceTypeSecrets,
	}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.NullableToOneMonitorInRequest{
			Data: &api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenSecret maps the API DataInSecretResponse onto the Terraform state model.
//
// Write-only value handling: the API never returns the `value` field on read.
// The model passed in already contains the prior state value (or the plan value
// for create/update). If the API response omits value (which it always does),
// the model's existing Value field is left unchanged so Terraform state retains
// the value that was set at create/update time.
func flattenSecret(_ context.Context, data api.DataInSecretResponse, model *secretResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Description != nil {
			model.Description = types.StringValue(*attrs.Description)
		} else {
			model.Description = types.StringNull()
		}

		if attrs.CreateDateTime != nil {
			model.CreateDateTime = types.StringValue(attrs.CreateDateTime.Format(time.RFC3339))
		} else {
			model.CreateDateTime = types.StringNull()
		}

		if attrs.UpdateDateTime != nil {
			model.UpdateDateTime = types.StringValue(attrs.UpdateDateTime.Format(time.RFC3339))
		} else {
			model.UpdateDateTime = types.StringNull()
		}

		// `value` is intentionally NOT set here. The API never returns the secret
		// value on read. The model already holds the prior state value (passed in
		// by Read) or the plan value (passed in by Create/Update), so leaving the
		// field unchanged preserves the correct value in Terraform state without
		// triggering spurious drift detection.
	}

	if data.Relationships != nil {
		rels := data.Relationships

		// monitor_id — nullable to-one
		if rels.Monitor != nil && rels.Monitor.Data != nil {
			model.MonitorID = types.StringValue(rels.Monitor.Data.Id)
		} else {
			model.MonitorID = types.StringNull()
		}
	} else {
		model.MonitorID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// SecretResourceModelForTest is a type alias for secretResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type SecretResourceModelForTest = secretResourceModel

// FlattenSecretForTest exposes flattenSecret for use in unit tests.
func FlattenSecretForTest(ctx context.Context, data api.DataInSecretResponse, model *secretResourceModel) diag.Diagnostics {
	return flattenSecret(ctx, data, model)
}

// BuildCreateSecretRelationshipsForTest exposes buildCreateSecretRelationships for use in unit tests.
func BuildCreateSecretRelationshipsForTest(ctx context.Context, plan secretResourceModel) (*api.RelationshipsInCreateSecretRequest, diag.Diagnostics) {
	return buildCreateSecretRelationships(ctx, plan)
}

// BuildUpdateSecretRelationshipsForTest exposes buildUpdateSecretRelationships for use in unit tests.
func BuildUpdateSecretRelationshipsForTest(ctx context.Context, plan secretResourceModel) (*api.RelationshipsInUpdateSecretRequest, diag.Diagnostics) {
	return buildUpdateSecretRelationships(ctx, plan)
}

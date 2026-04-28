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
	_ resource.Resource                = &sensorResource{}
	_ resource.ResourceWithConfigure   = &sensorResource{}
	_ resource.ResourceWithImportState = &sensorResource{}
)

// NewSensorResource returns a new sensorResource constructor function.
func NewSensorResource() resource.Resource {
	return &sensorResource{}
}

// sensorResource implements the monotaur_sensor managed resource.
type sensorResource struct {
	client *client.Client
}

// sensorResourceModel is the Terraform state model for a sensor.
//
// Attribute mapping:
//
//	AttributesInCreateSensorRequest:  name (required), pluginName (required), type (required), parameters (optional)
//	AttributesInSensorResponse:       id (computed), name, pluginName, type, parameters, createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*SensorRequest:    probe_id (to-one, required on create)
type sensorResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	PluginName     types.String `tfsdk:"plugin_name"`
	Type           types.String `tfsdk:"type"`
	Parameters     types.String `tfsdk:"parameters"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
	ProbeID        types.String `tfsdk:"probe_id"`
}

// Metadata sets the resource type name.
func (r *sensorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sensor"
}

// Schema defines the Terraform schema for the resource.
func (r *sensorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur sensor. Sensors collect data for a probe using a named plugin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the sensor (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the sensor.",
				Required:            true,
			},
			"plugin_name": schema.StringAttribute{
				MarkdownDescription: "The name of the plugin that powers this sensor.",
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The type identifier of the sensor.",
				Required:            true,
			},
			"parameters": schema.StringAttribute{
				MarkdownDescription: "JSON-encoded parameters for the sensor plugin.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the sensor was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the sensor was last updated (assigned by the API).",
				Computed:            true,
			},
			"probe_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the probe this sensor belongs to.",
				Required:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *sensorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new sensor via POST /sensors.
func (r *sensorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sensorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateSensorRequest{
		OpenapiDiscriminator: api.ResourceTypeSensors,
		Name:                 plan.Name.ValueString(),
		PluginName:           plan.PluginName.ValueString(),
		Type:                 plan.Type.ValueString(),
	}

	if !plan.Parameters.IsNull() && !plan.Parameters.IsUnknown() {
		attrs.Parameters = plan.Parameters.ValueString()
	}

	rels, diags := buildCreateSensorRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateSensorRequestDocument{
		Data: api.DataInCreateSensorRequest{
			Type:          api.ResourceTypeSensors,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_sensor: creating sensor", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostSensorWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostSensorParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Sensor", "Could not create sensor: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Sensor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSensorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenSensor(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_sensor: created sensor", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *sensorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sensorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_sensor: reading sensor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetSensor(ctx, id, &api.GetSensorParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor", "Could not read sensor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSensorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenSensor(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing sensor via PATCH /sensors/{id}.
func (r *sensorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sensorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state sensorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	nameVal := plan.Name.ValueString()
	pluginNameVal := plan.PluginName.ValueString()
	typeVal := plan.Type.ValueString()
	attrs := &api.AttributesInUpdateSensorRequest{
		OpenapiDiscriminator: api.ResourceTypeSensors,
		Name:                 &nameVal,
		PluginName:           &pluginNameVal,
		Type:                 &typeVal,
	}

	if !plan.Parameters.IsNull() && !plan.Parameters.IsUnknown() {
		attrs.Parameters = plan.Parameters.ValueString()
	}

	rels, diags := buildUpdateSensorRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateSensorRequestDocument{
		Data: api.DataInUpdateSensorRequest{
			Id:            id,
			Type:          api.ResourceTypeSensors,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_sensor: updating sensor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchSensorWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchSensorParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Sensor", "Could not update sensor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Sensor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInSensorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Sensor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenSensor(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_sensor: updated sensor", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a sensor via DELETE /sensors/{id}.
func (r *sensorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sensorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_sensor: deleting sensor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteSensor(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Sensor", "Could not delete sensor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Sensor", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_sensor: deleted sensor", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_sensor.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *sensorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateSensorRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateSensorRelationships(_ context.Context, plan sensorResourceModel) (*api.RelationshipsInCreateSensorRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateSensorRequest{
		OpenapiDiscriminator: api.ResourceTypeSensors,
		Probe: api.ToOneProbeInRequest{
			Data: api.ProbeIdentifierInRequest{
				Id:   plan.ProbeID.ValueString(),
				Type: api.ResourceTypeProbes,
			},
		},
	}

	return rels, diags
}

// buildUpdateSensorRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateSensorRelationships(_ context.Context, plan sensorResourceModel) (*api.RelationshipsInUpdateSensorRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInUpdateSensorRequest{
		OpenapiDiscriminator: api.ResourceTypeSensors,
	}

	if !plan.ProbeID.IsNull() && !plan.ProbeID.IsUnknown() {
		rels.Probe = &api.ToOneProbeInRequest{
			Data: api.ProbeIdentifierInRequest{
				Id:   plan.ProbeID.ValueString(),
				Type: api.ResourceTypeProbes,
			},
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenSensor maps the API DataInSensorResponse onto the Terraform state model.
func flattenSensor(_ context.Context, data api.DataInSensorResponse, model *sensorResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.PluginName != nil {
			model.PluginName = types.StringValue(*attrs.PluginName)
		} else {
			model.PluginName = types.StringNull()
		}

		if attrs.Type != nil {
			model.Type = types.StringValue(*attrs.Type)
		} else {
			model.Type = types.StringNull()
		}

		if attrs.Parameters != nil {
			// Parameters is interface{} — marshal to JSON string for storage in state.
			switch v := attrs.Parameters.(type) {
			case string:
				model.Parameters = types.StringValue(v)
			default:
				model.Parameters = types.StringNull()
			}
		} else {
			model.Parameters = types.StringNull()
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
	}

	if data.Relationships != nil {
		rels := data.Relationships

		// probe_id — to-one
		if rels.Probe != nil && rels.Probe.Data != nil {
			model.ProbeID = types.StringValue(rels.Probe.Data.Id)
		} else {
			model.ProbeID = types.StringNull()
		}
	} else {
		model.ProbeID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// SensorResourceModelForTest is a type alias for sensorResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type SensorResourceModelForTest = sensorResourceModel

// FlattenSensorForTest exposes flattenSensor for use in unit tests.
func FlattenSensorForTest(ctx context.Context, data api.DataInSensorResponse, model *sensorResourceModel) diag.Diagnostics {
	return flattenSensor(ctx, data, model)
}

// BuildCreateSensorRelationshipsForTest exposes buildCreateSensorRelationships for use in unit tests.
func BuildCreateSensorRelationshipsForTest(ctx context.Context, plan sensorResourceModel) (*api.RelationshipsInCreateSensorRequest, diag.Diagnostics) {
	return buildCreateSensorRelationships(ctx, plan)
}

// BuildUpdateSensorRelationshipsForTest exposes buildUpdateSensorRelationships for use in unit tests.
func BuildUpdateSensorRelationshipsForTest(ctx context.Context, plan sensorResourceModel) (*api.RelationshipsInUpdateSensorRequest, diag.Diagnostics) {
	return buildUpdateSensorRelationships(ctx, plan)
}

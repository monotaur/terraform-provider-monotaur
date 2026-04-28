package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &probeResource{}
	_ resource.ResourceWithConfigure   = &probeResource{}
	_ resource.ResourceWithImportState = &probeResource{}
)

// NewProbeResource returns a new probeResource constructor function.
func NewProbeResource() resource.Resource {
	return &probeResource{}
}

// probeResource implements the monotaur_probe managed resource.
type probeResource struct {
	client *client.Client
}

// probeResourceModel is the Terraform state model for a probe.
//
// Attribute mapping:
//
//	AttributesInCreateProbeRequest:  active (optional), schedule (optional)
//	AttributesInProbeResponse:       id (computed), active, schedule, createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*ProbeRequest:    monitor_id (to-one, required on create), sensor_ids (to-many, optional)
type probeResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Active         types.Bool   `tfsdk:"active"`
	Schedule       types.String `tfsdk:"schedule"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
	MonitorID      types.String `tfsdk:"monitor_id"`
	SensorIDs      types.List   `tfsdk:"sensor_ids"`
}

// Metadata sets the resource type name.
func (r *probeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_probe"
}

// Schema defines the Terraform schema for the resource.
func (r *probeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur probe. Probes collect data from sensors and associate them with a monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the probe (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the probe is active.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"schedule": schema.StringAttribute{
				MarkdownDescription: "The schedule expression for the probe (e.g. a cron expression).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the probe was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the probe was last updated (assigned by the API).",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this probe belongs to.",
				Required:            true,
			},
			"sensor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of sensors associated with this probe.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *probeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new probe via POST /probes.
func (r *probeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan probeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateProbeRequest{
		OpenapiDiscriminator: api.ResourceTypeProbes,
	}

	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		v := plan.Active.ValueBool()
		attrs.Active = &v
	}

	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		s := plan.Schedule.ValueString()
		attrs.Schedule = &s
	}

	rels, diags := buildCreateProbeRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateProbeRequestDocument{
		Data: api.DataInCreateProbeRequest{
			Type:          api.ResourceTypeProbes,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_probe: creating probe", map[string]any{"monitor_id": plan.MonitorID.ValueString()})

	apiResp, err := r.client.Inner().PostProbeWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostProbeParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Probe", "Could not create probe: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Probe", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInProbeResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenProbe(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_probe: created probe", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *probeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state probeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_probe: reading probe", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetProbe(ctx, id, &api.GetProbeParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe", "Could not read probe "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Probe", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInProbeResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenProbe(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing probe via PATCH /probes/{id}.
func (r *probeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan probeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state probeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateProbeRequest{
		OpenapiDiscriminator: api.ResourceTypeProbes,
	}

	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		v := plan.Active.ValueBool()
		attrs.Active = &v
	}

	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		s := plan.Schedule.ValueString()
		attrs.Schedule = &s
	}

	rels, diags := buildUpdateProbeRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateProbeRequestDocument{
		Data: api.DataInUpdateProbeRequest{
			Id:            id,
			Type:          api.ResourceTypeProbes,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_probe: updating probe", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchProbeWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchProbeParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Probe", "Could not update probe "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Probe", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInProbeResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Probe Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenProbe(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_probe: updated probe", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a probe via DELETE /probes/{id}.
func (r *probeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state probeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_probe: deleting probe", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteProbe(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Probe", "Could not delete probe "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Probe", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_probe: deleted probe", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_probe.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *probeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateProbeRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateProbeRelationships(ctx context.Context, plan probeResourceModel) (*api.RelationshipsInCreateProbeRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateProbeRequest{
		OpenapiDiscriminator: api.ResourceTypeProbes,
		Monitor: api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		},
	}

	// sensor_ids — to-many
	if !plan.SensorIDs.IsNull() && !plan.SensorIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.SensorIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.SensorIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.SensorIdentifierInRequest{Id: id, Type: api.ResourceTypeSensors}
			}
			rels.Sensors = &api.ToManySensorInRequest{Data: data}
		}
	}

	return rels, diags
}

// buildUpdateProbeRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateProbeRelationships(ctx context.Context, plan probeResourceModel) (*api.RelationshipsInUpdateProbeRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInUpdateProbeRequest{
		OpenapiDiscriminator: api.ResourceTypeProbes,
	}
	var diags diag.Diagnostics

	// monitor_id — to-one
	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		}
	}

	// sensor_ids — to-many
	if !plan.SensorIDs.IsNull() && !plan.SensorIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.SensorIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.SensorIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.SensorIdentifierInRequest{Id: id, Type: api.ResourceTypeSensors}
			}
			rels.Sensors = &api.ToManySensorInRequest{Data: data}
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenProbe maps the API DataInProbeResponse onto the Terraform state model.
func flattenProbe(ctx context.Context, data api.DataInProbeResponse, model *probeResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Active != nil {
			model.Active = types.BoolValue(*attrs.Active)
		} else {
			model.Active = types.BoolNull()
		}

		if attrs.Schedule != nil {
			model.Schedule = types.StringValue(*attrs.Schedule)
		} else {
			model.Schedule = types.StringNull()
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

		// monitor_id — to-one
		if rels.Monitor != nil && rels.Monitor.Data != nil {
			model.MonitorID = types.StringValue(rels.Monitor.Data.Id)
		} else {
			model.MonitorID = types.StringNull()
		}

		// sensor_ids — to-many
		if rels.Sensors != nil && rels.Sensors.Data != nil {
			ids := make([]string, len(*rels.Sensors.Data))
			for i, item := range *rels.Sensors.Data {
				ids[i] = item.Id
			}
			list, d := types.ListValueFrom(ctx, types.StringType, ids)
			diags.Append(d...)
			model.SensorIDs = list
		} else {
			model.SensorIDs = types.ListValueMust(types.StringType, nil)
		}
	} else {
		model.MonitorID = types.StringNull()
		model.SensorIDs = types.ListValueMust(types.StringType, nil)
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// ProbeResourceModelForTest is a type alias for probeResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type ProbeResourceModelForTest = probeResourceModel

// FlattenProbeForTest exposes flattenProbe for use in unit tests.
func FlattenProbeForTest(ctx context.Context, data api.DataInProbeResponse, model *probeResourceModel) diag.Diagnostics {
	return flattenProbe(ctx, data, model)
}

// BuildCreateProbeRelationshipsForTest exposes buildCreateProbeRelationships for use in unit tests.
func BuildCreateProbeRelationshipsForTest(ctx context.Context, plan probeResourceModel) (*api.RelationshipsInCreateProbeRequest, diag.Diagnostics) {
	return buildCreateProbeRelationships(ctx, plan)
}

// BuildUpdateProbeRelationshipsForTest exposes buildUpdateProbeRelationships for use in unit tests.
func BuildUpdateProbeRelationshipsForTest(ctx context.Context, plan probeResourceModel) (*api.RelationshipsInUpdateProbeRequest, diag.Diagnostics) {
	return buildUpdateProbeRelationships(ctx, plan)
}

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = &alarmResource{}
	_ resource.ResourceWithConfigure   = &alarmResource{}
	_ resource.ResourceWithImportState = &alarmResource{}
)

// NewAlarmResource returns a new alarmResource constructor function.
func NewAlarmResource() resource.Resource {
	return &alarmResource{}
}

// alarmResource implements the monotaur_alarm managed resource.
type alarmResource struct {
	client *client.Client
}

// alarmResourceModel is the Terraform state model for an alarm.
//
// Attribute mapping:
//
//	AttributesInCreateAlarmRequest:  startDateTime (optional), endDateTime (optional),
//	                                  excludeFromDowntime (optional), squelch (optional)
//	AttributesInAlarmResponse:       id (computed), startDateTime, endDateTime,
//	                                  excludeFromDowntime, squelch,
//	                                  createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*AlarmRequest:    monitor_id (to-one, required on create)
type alarmResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	StartDateTime      types.String `tfsdk:"start_date_time"`
	EndDateTime        types.String `tfsdk:"end_date_time"`
	ExcludeFromDowntime types.Bool  `tfsdk:"exclude_from_downtime"`
	Squelch            types.Bool   `tfsdk:"squelch"`
	CreateDateTime     types.String `tfsdk:"create_date_time"`
	UpdateDateTime     types.String `tfsdk:"update_date_time"`
	MonitorID          types.String `tfsdk:"monitor_id"`
}

// Metadata sets the resource type name.
func (r *alarmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alarm"
}

// Schema defines the Terraform schema for the resource.
func (r *alarmResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur alarm. Alarms represent detected problem periods for a monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the alarm (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"start_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm started.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"end_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm ended.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"exclude_from_downtime": schema.BoolAttribute{
				MarkdownDescription: "Whether this alarm is excluded from downtime calculations.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"squelch": schema.BoolAttribute{
				MarkdownDescription: "Whether this alarm is squelched (suppressed from alerting).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the alarm was last updated (assigned by the API).",
				Computed:            true,
			},
			"monitor_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the monitor this alarm belongs to.",
				Required:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *alarmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new alarm via POST /alarms.
func (r *alarmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alarmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateAlarmRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarms,
	}

	if !plan.StartDateTime.IsNull() && !plan.StartDateTime.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.StartDateTime.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid start_date_time", "Could not parse start_date_time as RFC3339: "+err.Error())
			return
		}
		attrs.StartDateTime = &t
	}

	if !plan.EndDateTime.IsNull() && !plan.EndDateTime.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.EndDateTime.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid end_date_time", "Could not parse end_date_time as RFC3339: "+err.Error())
			return
		}
		attrs.EndDateTime = &t
	}

	if !plan.ExcludeFromDowntime.IsNull() && !plan.ExcludeFromDowntime.IsUnknown() {
		v := plan.ExcludeFromDowntime.ValueBool()
		attrs.ExcludeFromDowntime = &v
	}

	if !plan.Squelch.IsNull() && !plan.Squelch.IsUnknown() {
		v := plan.Squelch.ValueBool()
		attrs.Squelch = &v
	}

	rels, diags := buildCreateAlarmRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateAlarmRequestDocument{
		Data: api.DataInCreateAlarmRequest{
			Type:          api.ResourceTypeAlarms,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_alarm: creating alarm", map[string]any{"monitor_id": plan.MonitorID.ValueString()})

	apiResp, err := r.client.Inner().PostAlarmWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAlarmParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Alarm", "Could not create alarm: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Alarm", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenAlarm(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_alarm: created alarm", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *alarmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alarmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm: reading alarm", map[string]any{"id": id})

	// Request the monitor relationship via `?include=monitor` so the response
	// populates Relationships.Monitor.Data. Without this, a bare GET on the
	// staging API echoes the relationship with `links` only, leaving monitor_id
	// null after import (see flattenAlarm).
	include := "monitor"
	query := map[string]*string{"include": &include}
	apiResp, err := r.client.Inner().GetAlarm(ctx, id, &api.GetAlarmParams{Query: &query})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm", "Could not read alarm "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenAlarm(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing alarm via PATCH /alarms/{id}.
func (r *alarmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan alarmResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state alarmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateAlarmRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarms,
	}

	if !plan.StartDateTime.IsNull() && !plan.StartDateTime.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.StartDateTime.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid start_date_time", "Could not parse start_date_time as RFC3339: "+err.Error())
			return
		}
		attrs.StartDateTime = &t
	}

	if !plan.EndDateTime.IsNull() && !plan.EndDateTime.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.EndDateTime.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid end_date_time", "Could not parse end_date_time as RFC3339: "+err.Error())
			return
		}
		attrs.EndDateTime = &t
	}

	if !plan.ExcludeFromDowntime.IsNull() && !plan.ExcludeFromDowntime.IsUnknown() {
		v := plan.ExcludeFromDowntime.ValueBool()
		attrs.ExcludeFromDowntime = &v
	}

	if !plan.Squelch.IsNull() && !plan.Squelch.IsUnknown() {
		v := plan.Squelch.ValueBool()
		attrs.Squelch = &v
	}

	rels, diags := buildUpdateAlarmRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateAlarmRequestDocument{
		Data: api.DataInUpdateAlarmRequest{
			Id:            id,
			Type:          api.ResourceTypeAlarms,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_alarm: updating alarm", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchAlarmWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchAlarmParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Alarm", "Could not update alarm "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Alarm", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetAlarm(ctx, id, &api.GetAlarmParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInAlarmResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenAlarm(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_alarm: updated alarm", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes an alarm via DELETE /alarms/{id}.
func (r *alarmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alarmResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm: deleting alarm", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteAlarm(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Alarm", "Could not delete alarm "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Alarm", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_alarm: deleted alarm", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_alarm.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *alarmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateAlarmRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateAlarmRelationships(_ context.Context, plan alarmResourceModel) (*api.RelationshipsInCreateAlarmRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInCreateAlarmRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarms,
		Monitor: api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
				Id:   plan.MonitorID.ValueString(),
				Type: api.ResourceTypeMonitors,
			},
		},
	}

	return rels, diags
}

// buildUpdateAlarmRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateAlarmRelationships(_ context.Context, plan alarmResourceModel) (*api.RelationshipsInUpdateAlarmRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	rels := &api.RelationshipsInUpdateAlarmRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarms,
	}

	if !plan.MonitorID.IsNull() && !plan.MonitorID.IsUnknown() {
		rels.Monitor = &api.ToOneMonitorInRequest{
			Data: api.MonitorIdentifierInRequest{
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

// flattenAlarm maps the API DataInAlarmResponse onto the Terraform state model.
func flattenAlarm(_ context.Context, data api.DataInAlarmResponse, model *alarmResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.StartDateTime != nil {
			model.StartDateTime = types.StringValue(attrs.StartDateTime.Format(time.RFC3339))
		} else {
			model.StartDateTime = types.StringNull()
		}

		if attrs.EndDateTime != nil {
			model.EndDateTime = types.StringValue(attrs.EndDateTime.Format(time.RFC3339))
		} else {
			model.EndDateTime = types.StringNull()
		}

		if attrs.ExcludeFromDowntime != nil {
			model.ExcludeFromDowntime = types.BoolValue(*attrs.ExcludeFromDowntime)
		} else {
			model.ExcludeFromDowntime = types.BoolNull()
		}

		if attrs.Squelch != nil {
			model.Squelch = types.BoolValue(*attrs.Squelch)
		} else {
			model.Squelch = types.BoolNull()
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

	// monitor_id — to-one. Preserve the prior model value when the API echoes
	// only `links` (no embedded `data`); overwriting with null would cause
	// "was X, but now null" inconsistency errors after a successful create.
	rels := data.Relationships
	if rels != nil && rels.Monitor != nil && rels.Monitor.Data != nil {
		model.MonitorID = types.StringValue(rels.Monitor.Data.Id)
	} else if model.MonitorID.IsUnknown() {
		model.MonitorID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// AlarmResourceModelForTest is a type alias for alarmResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type AlarmResourceModelForTest = alarmResourceModel

// FlattenAlarmForTest exposes flattenAlarm for use in unit tests.
func FlattenAlarmForTest(ctx context.Context, data api.DataInAlarmResponse, model *alarmResourceModel) diag.Diagnostics {
	return flattenAlarm(ctx, data, model)
}

// BuildCreateAlarmRelationshipsForTest exposes buildCreateAlarmRelationships for use in unit tests.
func BuildCreateAlarmRelationshipsForTest(ctx context.Context, plan alarmResourceModel) (*api.RelationshipsInCreateAlarmRequest, diag.Diagnostics) {
	return buildCreateAlarmRelationships(ctx, plan)
}

// BuildUpdateAlarmRelationshipsForTest exposes buildUpdateAlarmRelationships for use in unit tests.
func BuildUpdateAlarmRelationshipsForTest(ctx context.Context, plan alarmResourceModel) (*api.RelationshipsInUpdateAlarmRequest, diag.Diagnostics) {
	return buildUpdateAlarmRelationships(ctx, plan)
}

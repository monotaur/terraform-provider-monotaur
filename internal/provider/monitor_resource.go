package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
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
	_ resource.Resource                = &monitorResource{}
	_ resource.ResourceWithConfigure   = &monitorResource{}
	_ resource.ResourceWithImportState = &monitorResource{}
)

// NewMonitorResource returns a new monitorResource constructor function.
func NewMonitorResource() resource.Resource {
	return &monitorResource{}
}

// monitorResource implements the monotaur_monitor managed resource.
type monitorResource struct {
	client *client.Client
}

// monitorResourceModel is the Terraform state model for a monitor.
//
// Attribute mapping:
//
//	AttributesInCreateMonitorRequest:  name (required), type, enabled, heartbeatInterval,
//	                                    heartbeatGrace, noDataStatus, noDataMissedIntervals,
//	                                    faultThreshold, recoveryThreshold, confirmationDelay,
//	                                    flappingThreshold (optional)
//	AttributesInMonitorResponse:       id (computed), createDateTime, updateDateTime, status,
//	                                    statusMessage, statusExpirationDateTime, noDataDeadlineDateTime,
//	                                    noDataDateTime, pendingStatus, pendingStatusCount,
//	                                    pendingStatusDateTime, flappingDateTime (computed)
//	RelationshipsIn*MonitorRequest:    component_ids, label_ids (to-many, optional)
type monitorResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Type                     types.String `tfsdk:"type"`
	CreateDateTime           types.String `tfsdk:"create_date_time"`
	UpdateDateTime           types.String `tfsdk:"update_date_time"`
	Status                   types.String `tfsdk:"status"`
	StatusMessage            types.String `tfsdk:"status_message"`
	StatusExpirationDateTime types.String `tfsdk:"status_expiration_date_time"`
	Enabled                  types.Bool   `tfsdk:"enabled"`
	HeartbeatInterval        types.String `tfsdk:"heartbeat_interval"`
	HeartbeatGrace           types.String `tfsdk:"heartbeat_grace"`
	NoDataStatus             types.String `tfsdk:"no_data_status"`
	NoDataMissedIntervals    types.Int64  `tfsdk:"no_data_missed_intervals"`
	NoDataDeadlineDateTime   types.String `tfsdk:"no_data_deadline_date_time"`
	NoDataDateTime           types.String `tfsdk:"no_data_date_time"`
	FaultThreshold           types.Int64  `tfsdk:"fault_threshold"`
	RecoveryThreshold        types.Int64  `tfsdk:"recovery_threshold"`
	ConfirmationDelay        types.String `tfsdk:"confirmation_delay"`
	PendingStatus            types.String `tfsdk:"pending_status"`
	PendingStatusCount       types.Int64  `tfsdk:"pending_status_count"`
	PendingStatusDateTime    types.String `tfsdk:"pending_status_date_time"`
	FlappingThreshold        types.Int64  `tfsdk:"flapping_threshold"`
	FlappingDateTime         types.String `tfsdk:"flapping_date_time"`
	ComponentIDs             types.List   `tfsdk:"component_ids"`
	LabelIDs                 types.List   `tfsdk:"label_ids"`
}

// Metadata sets the resource type name.
func (r *monitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

// Schema defines the Terraform schema for the resource.
func (r *monitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur monitor. Monitors represent checks or probes that track the health of a component.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the monitor (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the monitor.",
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The monitor type (e.g. `Alert`, `Heartbeat`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor was last updated (assigned by the API).",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The current status of the monitor (system-managed, read-only).",
				Computed:            true,
			},
			"status_message": schema.StringAttribute{
				MarkdownDescription: "A message describing the current status (system-managed, read-only).",
				Computed:            true,
			},
			"status_expiration_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the current status expires (system-managed, read-only).",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the monitor is running. A paused (`false`) monitor stops its probe, ignores heartbeats and samples, opens no alarms and sends no notifications, and the paused period is excluded from uptime. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"heartbeat_interval": schema.StringAttribute{
				MarkdownDescription: "For `Heartbeat` monitors, the expected time between heartbeats, as a .NET TimeSpan (`hh:mm:ss` or `d.hh:mm:ss`, e.g. `00:05:00`).",
				Optional:            true,
			},
			"heartbeat_grace": schema.StringAttribute{
				MarkdownDescription: "For `Heartbeat` monitors, extra tolerance added to `heartbeat_interval` before a missed heartbeat faults the monitor (`hh:mm:ss` or `d.hh:mm:ss`).",
				Optional:            true,
			},
			"no_data_status": schema.StringAttribute{
				MarkdownDescription: "For a monitor with a sampling probe, the status the monitor moves to when its probe stops delivering samples (`Unknown`, `Warning` or `Fault`; `Ok` is rejected). Unset uses the server default (`Unknown` unless configured).",
				Optional:            true,
			},
			"no_data_missed_intervals": schema.Int64Attribute{
				MarkdownDescription: "For a monitor with a sampling probe, how many consecutive scheduled probe runs may pass without a sample before the monitor moves to `no_data_status` (0–1000; `0` turns the check off). Unset uses the server default (3 unless configured).",
				Optional:            true,
			},
			"no_data_deadline_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor goes to `no_data_status` unless a sample arrives first (system-managed, read-only).",
				Computed:            true,
			},
			"no_data_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor entered the no-data state; null while samples are arriving (system-managed, read-only).",
				Computed:            true,
			},
			"fault_threshold": schema.Int64Attribute{
				MarkdownDescription: "How many consecutive `Fault` samples are needed before the monitor becomes `Fault` and opens an alarm (1–100). Defaults to `1`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"recovery_threshold": schema.Int64Attribute{
				MarkdownDescription: "How many consecutive `Ok`/`Warning` samples are needed before a faulted monitor recovers and closes its alarm (1–100). Defaults to `1`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"confirmation_delay": schema.StringAttribute{
				MarkdownDescription: "How long a transition held back by `fault_threshold`/`recovery_threshold` must also have been reported before the monitor takes it (`00:00:01` to `1.00:00:00`). Unset waits for the sample count only.",
				Optional:            true,
			},
			"pending_status": schema.StringAttribute{
				MarkdownDescription: "The status recent samples report that the monitor has not taken yet because a threshold is not reached (system-managed, read-only).",
				Computed:            true,
			},
			"pending_status_count": schema.Int64Attribute{
				MarkdownDescription: "How many consecutive samples have reported `pending_status` (system-managed, read-only).",
				Computed:            true,
			},
			"pending_status_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the first sample of the pending run ended (system-managed, read-only).",
				Computed:            true,
			},
			"flapping_threshold": schema.Int64Attribute{
				MarkdownDescription: "Turns flapping detection on: the monitor is flapping once its last 20 samples switch between `Fault` and `Ok`/`Warning` at least this many times (2–19). Unset turns detection off.",
				Optional:            true,
			},
			"flapping_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the monitor started flapping; null while it is not flapping (system-managed, read-only).",
				Computed:            true,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components associated with this monitor.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels attached to this monitor.",
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
func (r *monitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new monitor via POST /monitors.
func (r *monitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateMonitorRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitors,
		Name:                 plan.Name.ValueString(),
	}

	if !plan.Type.IsNull() && !plan.Type.IsUnknown() {
		t := api.MonitorType(plan.Type.ValueString())
		attrs.Type = &t
	}

	settings := expandMonitorSettings(plan)
	attrs.Enabled = settings.Enabled
	attrs.HeartbeatInterval = settings.HeartbeatInterval
	attrs.HeartbeatGrace = settings.HeartbeatGrace
	attrs.NoDataStatus = settings.NoDataStatus
	attrs.NoDataMissedIntervals = settings.NoDataMissedIntervals
	attrs.FaultThreshold = settings.FaultThreshold
	attrs.RecoveryThreshold = settings.RecoveryThreshold
	attrs.ConfirmationDelay = settings.ConfirmationDelay
	attrs.FlappingThreshold = settings.FlappingThreshold

	rels, diags := buildCreateMonitorRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateMonitorRequestDocument{
		Data: api.DataInCreateMonitorRequest{
			Type:          api.ResourceTypeMonitors,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_monitor: creating monitor", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostMonitorWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostMonitorParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Monitor", "Could not create monitor: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Monitor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitor(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_monitor: created monitor", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *monitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor: reading monitor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetMonitor(ctx, id, &api.GetMonitorParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor", "Could not read monitor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMonitorResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitor(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing monitor via PATCH /monitors/{id}.
func (r *monitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan monitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state monitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	nameVal := plan.Name.ValueString()
	attrs := &api.AttributesInUpdateMonitorRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitors,
		Name:                 &nameVal,
	}

	if !plan.Type.IsNull() && !plan.Type.IsUnknown() {
		t := api.MonitorType(plan.Type.ValueString())
		attrs.Type = &t
	}

	settings := expandMonitorSettings(plan)
	attrs.Enabled = settings.Enabled
	attrs.HeartbeatInterval = settings.HeartbeatInterval
	attrs.HeartbeatGrace = settings.HeartbeatGrace
	attrs.NoDataStatus = settings.NoDataStatus
	attrs.NoDataMissedIntervals = settings.NoDataMissedIntervals
	attrs.FaultThreshold = settings.FaultThreshold
	attrs.RecoveryThreshold = settings.RecoveryThreshold
	attrs.ConfirmationDelay = settings.ConfirmationDelay
	attrs.FlappingThreshold = settings.FlappingThreshold

	var config monitorResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rels, diags := buildUpdateMonitorRelationships(ctx, configuredMonitorRelationships(plan, config))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateMonitorRequestDocument{
		Data: api.DataInUpdateMonitorRequest{
			Id:            id,
			Type:          api.ResourceTypeMonitors,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	// Settings removed from the configuration are sent as explicit nulls so
	// the API clears them; an omitted attribute would mean "no change".
	reqBody, err := client.MarshalWithNullAttributes(body, settings.Nulls)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Monitor", "Could not encode request: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_monitor: updating monitor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchMonitorWithBody(ctx, id, &api.PatchMonitorParams{}, client.ContentType, reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Monitor", "Could not update monitor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Monitor", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetMonitor(ctx, id, &api.GetMonitorParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInMonitorResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Monitor Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMonitor(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_monitor: updated monitor", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a monitor via DELETE /monitors/{id}.
func (r *monitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_monitor: deleting monitor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteMonitor(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Monitor", "Could not delete monitor "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the monitor was already deleted (e.g. cascade-deleted when a
		// child resource was removed). Treat as a no-op so destroy stays idempotent.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_monitor: monitor already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Monitor", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_monitor: deleted monitor", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_monitor.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *monitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// monitorSettings holds the optional monitor attributes shared by the create
// and update requests.
type monitorSettings struct {
	Enabled               *bool
	HeartbeatInterval     *string
	HeartbeatGrace        *string
	NoDataStatus          *api.MonitorStatus
	NoDataMissedIntervals *int32
	FaultThreshold        *int32
	RecoveryThreshold     *int32
	ConfirmationDelay     *string
	FlappingThreshold     *int32
	// Nulls lists the JSON names of the nullable attributes the plan leaves
	// unset, which an update must send as explicit nulls to clear.
	Nulls []string
}

// expandMonitorSettings reads the optional monitor attributes from the plan.
func expandMonitorSettings(plan monitorResourceModel) monitorSettings {
	var s monitorSettings

	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		s.Enabled = &v
	}
	s.FaultThreshold = int64ToInt32Ptr(plan.FaultThreshold)
	s.RecoveryThreshold = int64ToInt32Ptr(plan.RecoveryThreshold)

	optionalString := func(v types.String, name string) *string {
		if v.IsUnknown() {
			return nil
		}
		if v.IsNull() {
			s.Nulls = append(s.Nulls, name)
			return nil
		}
		str := v.ValueString()
		return &str
	}
	optionalInt := func(v types.Int64, name string) *int32 {
		if v.IsNull() && !v.IsUnknown() {
			s.Nulls = append(s.Nulls, name)
		}
		return int64ToInt32Ptr(v)
	}

	s.HeartbeatInterval = optionalString(plan.HeartbeatInterval, "heartbeatInterval")
	s.HeartbeatGrace = optionalString(plan.HeartbeatGrace, "heartbeatGrace")
	if v := optionalString(plan.NoDataStatus, "noDataStatus"); v != nil {
		st := api.MonitorStatus(*v)
		s.NoDataStatus = &st
	}
	s.NoDataMissedIntervals = optionalInt(plan.NoDataMissedIntervals, "noDataMissedIntervals")
	s.ConfirmationDelay = optionalString(plan.ConfirmationDelay, "confirmationDelay")
	s.FlappingThreshold = optionalInt(plan.FlappingThreshold, "flappingThreshold")

	return s
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateMonitorRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateMonitorRelationships(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInCreateMonitorRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInCreateMonitorRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitors,
	}
	var diags diag.Diagnostics

	// component_ids — to-many
	if !plan.ComponentIDs.IsNull() && !plan.ComponentIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.ComponentIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.ComponentIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.ComponentIdentifierInRequest{Id: id, Type: api.ResourceTypeComponents}
			}
			rels.Components = &api.ToManyComponentInRequest{Data: data}
		}
	}

	// label_ids — to-many
	if !plan.LabelIDs.IsNull() && !plan.LabelIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.LabelIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.LabelIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.LabelIdentifierInRequest{Id: id, Type: api.ResourceTypeLabels}
			}
			rels.Labels = &api.ToManyLabelInRequest{Data: data}
		}
	}

	return rels, diags
}

// configuredMonitorRelationships returns plan with the to-many relationship
// lists the configuration leaves out set to null, so the update request omits
// them. An unconfigured Optional+Computed list plans as its prior state, and
// sending it would replace links made elsewhere, such as
// monotaur_component.monitor_ids or monotaur_label.monitor_ids.
func configuredMonitorRelationships(plan, config monitorResourceModel) monitorResourceModel {
	if config.ComponentIDs.IsNull() {
		plan.ComponentIDs = types.ListNull(types.StringType)
	}
	if config.LabelIDs.IsNull() {
		plan.LabelIDs = types.ListNull(types.StringType)
	}
	return plan
}

// buildUpdateMonitorRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateMonitorRelationships(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInUpdateMonitorRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInUpdateMonitorRequest{
		OpenapiDiscriminator: api.ResourceTypeMonitors,
	}
	var diags diag.Diagnostics

	// component_ids — to-many
	if !plan.ComponentIDs.IsNull() && !plan.ComponentIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.ComponentIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.ComponentIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.ComponentIdentifierInRequest{Id: id, Type: api.ResourceTypeComponents}
			}
			rels.Components = &api.ToManyComponentInRequest{Data: data}
		}
	}

	// label_ids — to-many
	if !plan.LabelIDs.IsNull() && !plan.LabelIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.LabelIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.LabelIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.LabelIdentifierInRequest{Id: id, Type: api.ResourceTypeLabels}
			}
			rels.Labels = &api.ToManyLabelInRequest{Data: data}
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenMonitor maps the API DataInMonitorResponse onto the Terraform state model.
func flattenMonitor(ctx context.Context, data api.DataInMonitorResponse, model *monitorResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}

		if attrs.Type != nil {
			model.Type = types.StringValue(string(*attrs.Type))
		} else {
			model.Type = types.StringNull()
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

		if attrs.Status != nil {
			model.Status = types.StringValue(string(*attrs.Status))
		} else {
			model.Status = types.StringNull()
		}

		if attrs.StatusMessage != nil {
			model.StatusMessage = types.StringValue(*attrs.StatusMessage)
		} else {
			model.StatusMessage = types.StringNull()
		}

		if attrs.StatusExpirationDateTime != nil {
			model.StatusExpirationDateTime = types.StringValue(attrs.StatusExpirationDateTime.Format(time.RFC3339))
		} else {
			model.StatusExpirationDateTime = types.StringNull()
		}

		if attrs.Enabled != nil {
			model.Enabled = types.BoolValue(*attrs.Enabled)
		} else {
			model.Enabled = types.BoolNull()
		}

		model.HeartbeatInterval = flattenTimeSpan(attrs.HeartbeatInterval, model.HeartbeatInterval)
		model.HeartbeatGrace = flattenTimeSpan(attrs.HeartbeatGrace, model.HeartbeatGrace)
		model.ConfirmationDelay = flattenTimeSpan(attrs.ConfirmationDelay, model.ConfirmationDelay)

		if attrs.NoDataStatus != nil {
			model.NoDataStatus = types.StringValue(string(*attrs.NoDataStatus))
		} else {
			model.NoDataStatus = types.StringNull()
		}
		model.NoDataMissedIntervals = int32PtrToInt64(attrs.NoDataMissedIntervals)
		model.NoDataDeadlineDateTime = timePtrToString(attrs.NoDataDeadlineDateTime)
		model.NoDataDateTime = timePtrToString(attrs.NoDataDateTime)

		model.FaultThreshold = int32PtrToInt64(attrs.FaultThreshold)
		model.RecoveryThreshold = int32PtrToInt64(attrs.RecoveryThreshold)

		if attrs.PendingStatus != nil {
			model.PendingStatus = types.StringValue(string(*attrs.PendingStatus))
		} else {
			model.PendingStatus = types.StringNull()
		}
		model.PendingStatusCount = int32PtrToInt64(attrs.PendingStatusCount)
		model.PendingStatusDateTime = timePtrToString(attrs.PendingStatusDateTime)

		model.FlappingThreshold = int32PtrToInt64(attrs.FlappingThreshold)
		model.FlappingDateTime = timePtrToString(attrs.FlappingDateTime)
	}

	// component_ids — to-many. JSON:API responses from the Monotaur API often
	// echo relationships with only `links` (no embedded `data`). Without `data`
	// we cannot know the current member set, so we preserve the prior model
	// value — the plan for create/update, or the state for read. If we instead
	// overwrote with an empty list, Terraform would complain that
	// "element 0 has vanished" after a create that successfully attached
	// components. The trade-off: we won't detect out-of-band membership
	// changes via the resource Read alone; the next plan will pick them up
	// once the API includes the full data block.
	if data.Relationships != nil && data.Relationships.Components != nil && data.Relationships.Components.Data != nil {
		ids := make([]string, len(*data.Relationships.Components.Data))
		for i, item := range *data.Relationships.Components.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.ComponentIDs = list
	} else if model.ComponentIDs.IsNull() || model.ComponentIDs.IsUnknown() {
		model.ComponentIDs = types.ListValueMust(types.StringType, nil)
	}

	// label_ids — to-many, same handling as component_ids above.
	if data.Relationships != nil && data.Relationships.Labels != nil && data.Relationships.Labels.Data != nil {
		ids := make([]string, len(*data.Relationships.Labels.Data))
		for i, item := range *data.Relationships.Labels.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.LabelIDs = list
	} else if model.LabelIDs.IsNull() || model.LabelIDs.IsUnknown() {
		model.LabelIDs = types.ListValueMust(types.StringType, nil)
	}

	return diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// MonitorResourceModelForTest is a type alias for monitorResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type MonitorResourceModelForTest = monitorResourceModel

// FlattenMonitorForTest exposes flattenMonitor for use in unit tests.
func FlattenMonitorForTest(ctx context.Context, data api.DataInMonitorResponse, model *monitorResourceModel) diag.Diagnostics {
	return flattenMonitor(ctx, data, model)
}

// ExpandMonitorSettingsForTest exposes expandMonitorSettings for use in unit tests.
func ExpandMonitorSettingsForTest(plan monitorResourceModel) monitorSettings {
	return expandMonitorSettings(plan)
}

// ConfiguredMonitorRelationshipsForTest exposes configuredMonitorRelationships for use in unit tests.
func ConfiguredMonitorRelationshipsForTest(plan, config monitorResourceModel) monitorResourceModel {
	return configuredMonitorRelationships(plan, config)
}

// BuildCreateMonitorRelationshipsForTest exposes buildCreateMonitorRelationships for use in unit tests.
func BuildCreateMonitorRelationshipsForTest(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInCreateMonitorRequest, diag.Diagnostics) {
	return buildCreateMonitorRelationships(ctx, plan)
}

// BuildUpdateMonitorRelationshipsForTest exposes buildUpdateMonitorRelationships for use in unit tests.
func BuildUpdateMonitorRelationshipsForTest(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInUpdateMonitorRequest, diag.Diagnostics) {
	return buildUpdateMonitorRelationships(ctx, plan)
}

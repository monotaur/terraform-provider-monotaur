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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                   = &maintenanceWindowResource{}
	_ resource.ResourceWithConfigure      = &maintenanceWindowResource{}
	_ resource.ResourceWithImportState    = &maintenanceWindowResource{}
	_ resource.ResourceWithValidateConfig = &maintenanceWindowResource{}
)

// maintenanceMaxDescriptionLength is the API's limit on a maintenance window's
// or schedule's description.
const maintenanceMaxDescriptionLength = 250

// maintenanceWindowReasons are the values of the API's MaintenanceWindowReason enum.
var maintenanceWindowReasons = []string{
	string(api.ScheduledMaintenance),
	string(api.EmergencyMaintenance),
	string(api.MonitoringIssue),
	string(api.ExternalIssue),
}

// NewMaintenanceWindowResource returns a new maintenanceWindowResource constructor function.
func NewMaintenanceWindowResource() resource.Resource {
	return &maintenanceWindowResource{}
}

// maintenanceWindowResource implements the monotaur_maintenance_window managed resource.
type maintenanceWindowResource struct {
	client *client.Client
}

// maintenanceWindowResourceModel is the Terraform state model for a maintenance window.
//
// Attribute mapping:
//
//	AttributesIn*MaintenanceWindowRequest:   description (required), startDateTime, endDateTime,
//	                                         reason, excludeFromDowntime, applyToComponentStatus,
//	                                         squelch
//	AttributesInMaintenanceWindowResponse:   the above plus createDateTime, updateDateTime (computed)
//	RelationshipsInMaintenanceWindowResponse: maintenanceSchedule (to-one, read-only here)
//
// The `components` relationship is deliberately not part of the schema:
// window↔component links are owned by monotaur_component.maintenance_window_ids,
// and managing them from both sides would make the two resources fight. The
// `maintenanceSchedule` link is exposed read-only: linking a one-off window to
// a schedule does not make the schedule manage it (only windows the schedule
// generated itself are schedule-owned), so there is nothing to configure.
type maintenanceWindowResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	Description            types.String `tfsdk:"description"`
	StartDateTime          types.String `tfsdk:"start_date_time"`
	EndDateTime            types.String `tfsdk:"end_date_time"`
	Reason                 types.String `tfsdk:"reason"`
	ExcludeFromDowntime    types.Bool   `tfsdk:"exclude_from_downtime"`
	ApplyToComponentStatus types.Bool   `tfsdk:"apply_to_component_status"`
	Squelch                types.Bool   `tfsdk:"squelch"`
	MaintenanceScheduleID  types.String `tfsdk:"maintenance_schedule_id"`
	CreateDateTime         types.String `tfsdk:"create_date_time"`
	UpdateDateTime         types.String `tfsdk:"update_date_time"`
}

// Metadata sets the resource type name.
func (r *maintenanceWindowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance_window"
}

// Schema defines the Terraform schema for the resource.
func (r *maintenanceWindowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a one-off Monotaur maintenance window. For recurring maintenance use " +
			"`monotaur_maintenance_schedule`, which generates its own windows. Attach the window to components with " +
			"`monotaur_component.maintenance_window_ids`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the maintenance window (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the maintenance (1 to 250 characters).",
				Required:            true,
				Validators: []validator.String{
					ncStringLengthBetween(1, maintenanceMaxDescriptionLength),
				},
			},
			"start_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the window starts, e.g. `2030-01-01T22:00:00Z`. " +
					"The API stores it in UTC; any offset may be used and is kept as written while the instant is unchanged.",
				Required: true,
				Validators: []validator.String{
					maintenanceRFC3339(),
				},
			},
			"end_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the window ends. Must not be before `start_date_time`.",
				Required:            true,
				Validators: []validator.String{
					maintenanceRFC3339(),
				},
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Why the maintenance happens: `ScheduledMaintenance` (the default), " +
					"`EmergencyMaintenance`, `MonitoringIssue` or `ExternalIssue`.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					ncStringOneOf(maintenanceWindowReasons...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"exclude_from_downtime": schema.BoolAttribute{
				MarkdownDescription: "Whether time inside the window is excluded from downtime in uptime reports. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"apply_to_component_status": schema.BoolAttribute{
				MarkdownDescription: "Whether the window's components show as under maintenance while it is active. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"squelch": schema.BoolAttribute{
				MarkdownDescription: "Whether alarm notifications for the window's components are suppressed while it is active. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_schedule_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the maintenance schedule the window is linked to, or null (read-only). " +
					"Windows managed by this resource are normally one-off; it is cleared when the schedule is deleted.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the window was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the window was last updated (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// ValidateConfig rejects a window that ends before it starts. The API checks
// this too, but only for the attributes a PATCH carries, so it is enforced here
// at plan time as well.
func (r *maintenanceWindowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var start, end types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("start_date_time"), &start)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("end_date_time"), &end)...)
	if resp.Diagnostics.HasError() || !ncKnownString(start) || !ncKnownString(end) {
		return
	}
	s, errS := time.Parse(time.RFC3339, start.ValueString())
	e, errE := time.Parse(time.RFC3339, end.ValueString())
	if errS != nil || errE != nil {
		return // reported by the attribute validators
	}
	if e.Before(s) {
		resp.Diagnostics.AddAttributeError(path.Root("end_date_time"), "Invalid Maintenance Window",
			fmt.Sprintf("end_date_time (%s) must not be before start_date_time (%s).", end.ValueString(), start.ValueString()))
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *maintenanceWindowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new maintenance window via POST /maintenanceWindows.
func (r *maintenanceWindowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan maintenanceWindowResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs, diags := buildCreateMaintenanceWindowAttributes(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateMaintenanceWindowRequestDocument{
		Data: api.DataInCreateMaintenanceWindowRequest{
			Type:       api.ResourceTypeMaintenanceWindows,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_maintenance_window: creating maintenance window", map[string]any{"description": plan.Description.ValueString()})

	params := &api.PostMaintenanceWindowParams{Query: maintenanceWindowIncludeQuery()}
	apiResp, err := r.client.Inner().PostMaintenanceWindowWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, params, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Maintenance Window", "Could not create maintenance window: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Maintenance Window", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMaintenanceWindowResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Window Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceWindow(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_window: created maintenance window", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *maintenanceWindowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state maintenanceWindowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_maintenance_window: reading maintenance window", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetMaintenanceWindow(ctx, id, maintenanceWindowReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Window", "Could not read maintenance window "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the window was deleted out-of-band — remove it from state so
		// the next plan re-creates it instead of erroring on a missing resource.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_maintenance_window: maintenance window not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Maintenance Window", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMaintenanceWindowResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Window Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceWindow(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing maintenance window via PATCH /maintenanceWindows/{id}.
func (r *maintenanceWindowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan maintenanceWindowResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state maintenanceWindowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs, diags := buildUpdateMaintenanceWindowAttributes(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateMaintenanceWindowRequestDocument{
		Data: api.DataInUpdateMaintenanceWindowRequest{
			Id:         id,
			Type:       api.ResourceTypeMaintenanceWindows,
			Attributes: attrs,
		},
	}

	tflog.Debug(ctx, "monotaur_maintenance_window: updating maintenance window", map[string]any{"id": id})

	params := &api.PatchMaintenanceWindowParams{Query: maintenanceWindowIncludeQuery()}
	apiResp, err := r.client.Inner().PatchMaintenanceWindowWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, params, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Maintenance Window", "Could not update maintenance window "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Maintenance Window", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetMaintenanceWindow(ctx, id, maintenanceWindowReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Window After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInMaintenanceWindowResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Window Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceWindow(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_window: updated maintenance window", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a maintenance window via DELETE /maintenanceWindows/{id}.
func (r *maintenanceWindowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state maintenanceWindowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_maintenance_window: deleting maintenance window", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteMaintenanceWindow(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Maintenance Window", "Could not delete maintenance window "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_maintenance_window: maintenance window already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Maintenance Window", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_window: deleted maintenance window", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_maintenance_window.example <id>`.
func (r *maintenanceWindowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// maintenanceWindowIncludeQuery requests the to-one maintenanceSchedule
// relationship via `?include=`, so its `data` is always present (null for a
// one-off window). Without it the API echoes `links` only, and an absent link
// could not be told apart from a cleared one.
func maintenanceWindowIncludeQuery() *map[string]*string {
	include := "maintenanceSchedule"
	return &map[string]*string{"include": &include}
}

// maintenanceWindowReadParams returns the GET parameters used by Read, the
// data source and the post-update re-fetch.
func maintenanceWindowReadParams() *api.GetMaintenanceWindowParams {
	return &api.GetMaintenanceWindowParams{Query: maintenanceWindowIncludeQuery()}
}

// parseMaintenanceTimestamp parses a configured RFC 3339 timestamp.
func parseMaintenanceTimestamp(attr string, v types.String) (*time.Time, diag.Diagnostics) {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() {
		return nil, diags
	}
	t, err := time.Parse(time.RFC3339, v.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root(attr), "Invalid RFC 3339 Timestamp", err.Error())
		return nil, diags
	}
	return &t, diags
}

// buildCreateMaintenanceWindowAttributes maps the plan onto the create attributes.
func buildCreateMaintenanceWindowAttributes(plan maintenanceWindowResourceModel) (*api.AttributesInCreateMaintenanceWindowRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	start, d := parseMaintenanceTimestamp("start_date_time", plan.StartDateTime)
	diags.Append(d...)
	end, d := parseMaintenanceTimestamp("end_date_time", plan.EndDateTime)
	diags.Append(d...)

	attrs := &api.AttributesInCreateMaintenanceWindowRequest{
		OpenapiDiscriminator:   api.ResourceTypeMaintenanceWindows,
		Description:            plan.Description.ValueString(),
		StartDateTime:          start,
		EndDateTime:            end,
		ExcludeFromDowntime:    maintenanceBoolPtr(plan.ExcludeFromDowntime),
		ApplyToComponentStatus: maintenanceBoolPtr(plan.ApplyToComponentStatus),
		Squelch:                maintenanceBoolPtr(plan.Squelch),
	}
	if !plan.Reason.IsNull() && !plan.Reason.IsUnknown() {
		v := api.MaintenanceWindowReason(plan.Reason.ValueString())
		attrs.Reason = &v
	}
	return attrs, diags
}

// buildUpdateMaintenanceWindowAttributes maps the plan onto the update
// attributes. Start and end are always sent together: the API only checks
// that the end is not before the start for the attributes a PATCH carries.
func buildUpdateMaintenanceWindowAttributes(plan maintenanceWindowResourceModel) (*api.AttributesInUpdateMaintenanceWindowRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	start, d := parseMaintenanceTimestamp("start_date_time", plan.StartDateTime)
	diags.Append(d...)
	end, d := parseMaintenanceTimestamp("end_date_time", plan.EndDateTime)
	diags.Append(d...)

	description := plan.Description.ValueString()
	attrs := &api.AttributesInUpdateMaintenanceWindowRequest{
		OpenapiDiscriminator:   api.ResourceTypeMaintenanceWindows,
		Description:            &description,
		StartDateTime:          start,
		EndDateTime:            end,
		ExcludeFromDowntime:    maintenanceBoolPtr(plan.ExcludeFromDowntime),
		ApplyToComponentStatus: maintenanceBoolPtr(plan.ApplyToComponentStatus),
		Squelch:                maintenanceBoolPtr(plan.Squelch),
	}
	if !plan.Reason.IsNull() && !plan.Reason.IsUnknown() {
		v := api.MaintenanceWindowReason(plan.Reason.ValueString())
		attrs.Reason = &v
	}
	return attrs, diags
}

// maintenanceBoolPtr returns a pointer to v's value, or nil when v is null or unknown.
func maintenanceBoolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// maintenanceBoolValue maps an optional API boolean onto a Terraform value.
func maintenanceBoolValue(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenMaintenanceTimestamp maps an API timestamp the provider sent onto a
// Terraform value. The API returns timestamps in UTC (e.g. "+02:00" input comes
// back as "+00:00"), so when prior holds the same instant in another spelling
// it is kept, avoiding a spurious diff or an "inconsistent result after apply"
// error. Otherwise the instant is formatted as RFC 3339 in UTC, with fractional
// seconds only when present.
func flattenMaintenanceTimestamp(v *time.Time, prior types.String) types.String {
	if v == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		if p, err := time.Parse(time.RFC3339, prior.ValueString()); err == nil && p.Equal(*v) {
			return prior
		}
	}
	return types.StringValue(v.UTC().Format(time.RFC3339Nano))
}

// flattenMaintenanceWindow maps the API DataInMaintenanceWindowResponse onto the Terraform state model.
func flattenMaintenanceWindow(_ context.Context, data api.DataInMaintenanceWindowResponse, model *maintenanceWindowResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Description != nil {
			model.Description = types.StringValue(*attrs.Description)
		} else {
			model.Description = types.StringNull()
		}

		model.StartDateTime = flattenMaintenanceTimestamp(attrs.StartDateTime, model.StartDateTime)
		model.EndDateTime = flattenMaintenanceTimestamp(attrs.EndDateTime, model.EndDateTime)

		if attrs.Reason != nil {
			model.Reason = types.StringValue(string(*attrs.Reason))
		} else {
			model.Reason = types.StringNull()
		}

		model.ExcludeFromDowntime = maintenanceBoolValue(attrs.ExcludeFromDowntime)
		model.ApplyToComponentStatus = maintenanceBoolValue(attrs.ApplyToComponentStatus)
		model.Squelch = maintenanceBoolValue(attrs.Squelch)
		model.CreateDateTime = timePtrToString(attrs.CreateDateTime)
		model.UpdateDateTime = timePtrToString(attrs.UpdateDateTime)
	}

	// Every request asks for `include=maintenanceSchedule`, so absent data
	// means the window is not linked to a schedule.
	if data.Relationships != nil && data.Relationships.MaintenanceSchedule != nil && data.Relationships.MaintenanceSchedule.Data != nil {
		model.MaintenanceScheduleID = types.StringValue(data.Relationships.MaintenanceSchedule.Data.Id)
	} else {
		model.MaintenanceScheduleID = types.StringNull()
	}

	return diags
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

type maintenanceRFC3339Validator struct{}

// maintenanceRFC3339 validates that a string is an RFC 3339 timestamp.
func maintenanceRFC3339() maintenanceRFC3339Validator { return maintenanceRFC3339Validator{} }

func (v maintenanceRFC3339Validator) Description(_ context.Context) string {
	return "value must be an RFC 3339 timestamp, e.g. 2030-01-01T22:00:00Z"
}

func (v maintenanceRFC3339Validator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v maintenanceRFC3339Validator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	if _, err := time.Parse(time.RFC3339, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not valid: %s.", req.ConfigValue.ValueString(), v.Description(ctx)))
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// MaintenanceWindowResourceModelForTest is a type alias for
// maintenanceWindowResourceModel for use in unit tests.
type MaintenanceWindowResourceModelForTest = maintenanceWindowResourceModel

// FlattenMaintenanceWindowForTest exposes flattenMaintenanceWindow for use in unit tests.
func FlattenMaintenanceWindowForTest(ctx context.Context, data api.DataInMaintenanceWindowResponse, model *maintenanceWindowResourceModel) diag.Diagnostics {
	return flattenMaintenanceWindow(ctx, data, model)
}

// BuildCreateMaintenanceWindowAttributesForTest exposes buildCreateMaintenanceWindowAttributes for use in unit tests.
func BuildCreateMaintenanceWindowAttributesForTest(plan maintenanceWindowResourceModel) (*api.AttributesInCreateMaintenanceWindowRequest, diag.Diagnostics) {
	return buildCreateMaintenanceWindowAttributes(plan)
}

// BuildUpdateMaintenanceWindowAttributesForTest exposes buildUpdateMaintenanceWindowAttributes for use in unit tests.
func BuildUpdateMaintenanceWindowAttributesForTest(plan maintenanceWindowResourceModel) (*api.AttributesInUpdateMaintenanceWindowRequest, diag.Diagnostics) {
	return buildUpdateMaintenanceWindowAttributes(plan)
}

// FlattenMaintenanceTimestampForTest exposes flattenMaintenanceTimestamp for use in unit tests.
func FlattenMaintenanceTimestampForTest(v *time.Time, prior types.String) types.String {
	return flattenMaintenanceTimestamp(v, prior)
}

// MaintenanceWindowReadParamsForTest exposes maintenanceWindowReadParams for use in unit tests.
func MaintenanceWindowReadParamsForTest() *api.GetMaintenanceWindowParams {
	return maintenanceWindowReadParams()
}

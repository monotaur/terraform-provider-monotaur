package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
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
	_ resource.Resource                = &maintenanceScheduleResource{}
	_ resource.ResourceWithConfigure   = &maintenanceScheduleResource{}
	_ resource.ResourceWithImportState = &maintenanceScheduleResource{}
)

// NewMaintenanceScheduleResource returns a new maintenanceScheduleResource constructor function.
func NewMaintenanceScheduleResource() resource.Resource {
	return &maintenanceScheduleResource{}
}

// maintenanceScheduleResource implements the monotaur_maintenance_schedule managed resource.
type maintenanceScheduleResource struct {
	client *client.Client
}

// maintenanceScheduleResourceModel is the Terraform state model for a maintenance schedule.
//
// Attribute mapping:
//
//	AttributesIn*MaintenanceScheduleRequest:  description, schedule (required), timeSpan,
//	                                          timeZoneId (nullable), excludeFromDowntime,
//	                                          applyToComponentStatus, squelch
//	AttributesInMaintenanceScheduleResponse:  the above plus createDateTime, updateDateTime (computed)
//	RelationshipsIn*MaintenanceScheduleRequest: component_ids (to-many)
//
// The read-only `maintenanceWindows` relationship is deliberately not part of
// the schema: the server materialises one window per cron occurrence (always
// one ahead), so the list changes on its own and would show as drift.
type maintenanceScheduleResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	Description            types.String `tfsdk:"description"`
	Schedule               types.String `tfsdk:"schedule"`
	TimeSpan               types.String `tfsdk:"time_span"`
	TimeZoneID             types.String `tfsdk:"time_zone_id"`
	ExcludeFromDowntime    types.Bool   `tfsdk:"exclude_from_downtime"`
	ApplyToComponentStatus types.Bool   `tfsdk:"apply_to_component_status"`
	Squelch                types.Bool   `tfsdk:"squelch"`
	CreateDateTime         types.String `tfsdk:"create_date_time"`
	UpdateDateTime         types.String `tfsdk:"update_date_time"`
	ComponentIDs           types.List   `tfsdk:"component_ids"`
}

// Metadata sets the resource type name.
func (r *maintenanceScheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance_schedule"
}

// Schema defines the Terraform schema for the resource.
func (r *maintenanceScheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a recurring Monotaur maintenance schedule. At each occurrence of the cron `schedule` " +
			"(evaluated in `time_zone_id`) the schedule has a maintenance window lasting `time_span`. The API creates these " +
			"windows itself, one occurrence ahead, copying the schedule's description, flags and components; they are not " +
			"managed by Terraform. Deleting the schedule deletes its not-yet-started window and keeps past ones.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the maintenance schedule (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the maintenance (1 to 250 characters), copied to every generated window.",
				Required:            true,
				Validators: []validator.String{
					ncStringLengthBetween(1, maintenanceMaxDescriptionLength),
				},
			},
			"schedule": schema.StringAttribute{
				MarkdownDescription: "A cron expression for when each window starts: five fields (e.g. `0 2 * * 0` for Sundays at 02:00), " +
					"six fields with a leading seconds field, or a macro such as `@daily`.",
				Required: true,
			},
			"time_span": schema.StringAttribute{
				MarkdownDescription: "How long each window lasts, as a positive .NET TimeSpan `[d.]hh:mm:ss`, e.g. `02:00:00` or `1.00:00:00` for a day. " +
					"The API normalises the spelling (`2:0:0` becomes `02:00:00`); an equal duration is not shown as a change.",
				Required: true,
				Validators: []validator.String{
					maintenancePositiveTimeSpan(),
				},
			},
			"time_zone_id": schema.StringAttribute{
				MarkdownDescription: "The time zone `schedule` is evaluated in, as an IANA ID such as `Europe/Berlin` (Windows IDs are also accepted " +
					"and stored as written). Unset means UTC.",
				Optional: true,
			},
			"exclude_from_downtime": schema.BoolAttribute{
				MarkdownDescription: "Whether time inside the generated windows is excluded from downtime in uptime reports. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"apply_to_component_status": schema.BoolAttribute{
				MarkdownDescription: "Whether the components show as under maintenance while a generated window is active. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"squelch": schema.BoolAttribute{
				MarkdownDescription: "Whether alarm notifications for the components are suppressed while a generated window is active. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the schedule was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the schedule was last updated (assigned by the API).",
				Computed:            true,
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the components the schedule's windows apply to. The upcoming generated window follows changes. Set to `[]` to remove all.",
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
func (r *maintenanceScheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new maintenance schedule via POST /maintenanceSchedules.
func (r *maintenanceScheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan maintenanceScheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, diags := buildMaintenanceScheduleComponents(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateMaintenanceScheduleRequestDocument{
		Data: api.DataInCreateMaintenanceScheduleRequest{
			Type:       api.ResourceTypeMaintenanceSchedules,
			Attributes: buildCreateMaintenanceScheduleAttributes(plan),
			Relationships: &api.RelationshipsInCreateMaintenanceScheduleRequest{
				OpenapiDiscriminator: api.ResourceTypeMaintenanceSchedules,
				Components:           components,
			},
		},
	}

	tflog.Debug(ctx, "monotaur_maintenance_schedule: creating maintenance schedule", map[string]any{"description": plan.Description.ValueString()})

	params := &api.PostMaintenanceScheduleParams{Query: maintenanceScheduleIncludeQuery()}
	apiResp, err := r.client.Inner().PostMaintenanceScheduleWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, params, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Maintenance Schedule", "Could not create maintenance schedule: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Maintenance Schedule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMaintenanceScheduleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceSchedule(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_schedule: created maintenance schedule", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *maintenanceScheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state maintenanceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_maintenance_schedule: reading maintenance schedule", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetMaintenanceSchedule(ctx, id, maintenanceScheduleReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule", "Could not read maintenance schedule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the schedule was deleted out-of-band — remove it from state so
		// the next plan re-creates it instead of erroring on a missing resource.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_maintenance_schedule: maintenance schedule not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInMaintenanceScheduleResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceSchedule(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing maintenance schedule via PATCH /maintenanceSchedules/{id}.
// The API then moves the upcoming generated window to the new settings.
func (r *maintenanceScheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan maintenanceScheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state maintenanceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	components, diags := buildMaintenanceScheduleComponents(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateMaintenanceScheduleRequestDocument{
		Data: api.DataInUpdateMaintenanceScheduleRequest{
			Id:         id,
			Type:       api.ResourceTypeMaintenanceSchedules,
			Attributes: buildUpdateMaintenanceScheduleAttributes(plan),
			Relationships: &api.RelationshipsInUpdateMaintenanceScheduleRequest{
				OpenapiDiscriminator: api.ResourceTypeMaintenanceSchedules,
				Components:           components,
			},
		},
	}

	// The generated attribute struct omits nil pointers, which JSON:API reads
	// as "no change". Send an explicit null for time_zone_id when the
	// configuration no longer sets it, so that removing it reverts to UTC.
	reader, err := client.MarshalWithNullAttributes(body, maintenanceScheduleNullAttributes(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Maintenance Schedule", "Could not encode request: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_schedule: updating maintenance schedule", map[string]any{"id": id})

	params := &api.PatchMaintenanceScheduleParams{Query: maintenanceScheduleIncludeQuery()}
	apiResp, err := r.client.Inner().PatchMaintenanceScheduleWithBody(ctx, id, params, client.ContentType, reader)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Maintenance Schedule", "Could not update maintenance schedule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Maintenance Schedule", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetMaintenanceSchedule(ctx, id, maintenanceScheduleReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInMaintenanceScheduleResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Maintenance Schedule Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenMaintenanceSchedule(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_schedule: updated maintenance schedule", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a maintenance schedule via DELETE /maintenanceSchedules/{id}.
// The API deletes the schedule's not-yet-started generated window and keeps
// started and past windows, clearing their schedule reference.
func (r *maintenanceScheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state maintenanceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_maintenance_schedule: deleting maintenance schedule", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteMaintenanceSchedule(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Maintenance Schedule", "Could not delete maintenance schedule "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_maintenance_schedule: maintenance schedule already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Maintenance Schedule", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_maintenance_schedule: deleted maintenance schedule", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_maintenance_schedule.example <id>`.
func (r *maintenanceScheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// maintenanceScheduleIncludeQuery requests the components relationship via
// `?include=` so the response carries its `data` (a bare request echoes
// `links` only, which would leave imported schedules without IDs and hide
// drift). The included collection is paginated by the API (default 10), so it
// is scoped to the maximum page size.
func maintenanceScheduleIncludeQuery() *map[string]*string {
	include := "components"
	pageSize := "components:500"
	return &map[string]*string{"include": &include, "page[size]": &pageSize}
}

// maintenanceScheduleReadParams returns the GET parameters used by Read, the
// data source and the post-update re-fetch.
func maintenanceScheduleReadParams() *api.GetMaintenanceScheduleParams {
	return &api.GetMaintenanceScheduleParams{Query: maintenanceScheduleIncludeQuery()}
}

// buildCreateMaintenanceScheduleAttributes maps the plan onto the create attributes.
func buildCreateMaintenanceScheduleAttributes(plan maintenanceScheduleResourceModel) *api.AttributesInCreateMaintenanceScheduleRequest {
	attrs := &api.AttributesInCreateMaintenanceScheduleRequest{
		OpenapiDiscriminator:   api.ResourceTypeMaintenanceSchedules,
		Description:            plan.Description.ValueString(),
		Schedule:               plan.Schedule.ValueString(),
		ExcludeFromDowntime:    maintenanceBoolPtr(plan.ExcludeFromDowntime),
		ApplyToComponentStatus: maintenanceBoolPtr(plan.ApplyToComponentStatus),
		Squelch:                maintenanceBoolPtr(plan.Squelch),
	}
	if !plan.TimeSpan.IsNull() && !plan.TimeSpan.IsUnknown() {
		v := plan.TimeSpan.ValueString()
		attrs.TimeSpan = &v
	}
	if !plan.TimeZoneID.IsNull() && !plan.TimeZoneID.IsUnknown() {
		v := plan.TimeZoneID.ValueString()
		attrs.TimeZoneId = &v
	}
	return attrs
}

// buildUpdateMaintenanceScheduleAttributes maps the plan onto the update
// attributes. A null time_zone_id is left nil here; see
// maintenanceScheduleNullAttributes for how it is cleared.
func buildUpdateMaintenanceScheduleAttributes(plan maintenanceScheduleResourceModel) *api.AttributesInUpdateMaintenanceScheduleRequest {
	description := plan.Description.ValueString()
	schedule := plan.Schedule.ValueString()
	attrs := &api.AttributesInUpdateMaintenanceScheduleRequest{
		OpenapiDiscriminator:   api.ResourceTypeMaintenanceSchedules,
		Description:            &description,
		Schedule:               &schedule,
		ExcludeFromDowntime:    maintenanceBoolPtr(plan.ExcludeFromDowntime),
		ApplyToComponentStatus: maintenanceBoolPtr(plan.ApplyToComponentStatus),
		Squelch:                maintenanceBoolPtr(plan.Squelch),
	}
	if !plan.TimeSpan.IsNull() && !plan.TimeSpan.IsUnknown() {
		v := plan.TimeSpan.ValueString()
		attrs.TimeSpan = &v
	}
	if !plan.TimeZoneID.IsNull() && !plan.TimeZoneID.IsUnknown() {
		v := plan.TimeZoneID.ValueString()
		attrs.TimeZoneId = &v
	}
	return attrs
}

// maintenanceScheduleNullAttributes returns the JSON names of the nullable,
// Optional (non-Computed) attributes whose plan value is null. They are sent
// as explicit JSON nulls on update so that removing them from configuration
// clears them on the server.
func maintenanceScheduleNullAttributes(plan maintenanceScheduleResourceModel) []string {
	var names []string
	if plan.TimeZoneID.IsNull() {
		names = append(names, "timeZoneId")
	}
	return names
}

// buildMaintenanceScheduleComponents converts component_ids into the request
// relationship. A null or unknown list is left out (no change); an empty list
// is sent as `"data": []`, which clears the relationship.
func buildMaintenanceScheduleComponents(ctx context.Context, plan maintenanceScheduleResourceModel) (*api.ToManyComponentInRequest, diag.Diagnostics) {
	ids, ok, diags := policyRelKnownIDs(ctx, plan.ComponentIDs)
	if diags.HasError() || !ok {
		return nil, diags
	}
	data := make([]api.ComponentIdentifierInRequest, len(ids))
	for i, id := range ids {
		data[i] = api.ComponentIdentifierInRequest{Id: id, Type: api.ResourceTypeComponents}
	}
	return &api.ToManyComponentInRequest{Data: data}, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenMaintenanceSchedule maps the API DataInMaintenanceScheduleResponse onto the Terraform state model.
func flattenMaintenanceSchedule(ctx context.Context, data api.DataInMaintenanceScheduleResponse, model *maintenanceScheduleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Description != nil {
			model.Description = types.StringValue(*attrs.Description)
		} else {
			model.Description = types.StringNull()
		}

		if attrs.Schedule != nil {
			model.Schedule = types.StringValue(*attrs.Schedule)
		} else {
			model.Schedule = types.StringNull()
		}

		model.TimeSpan = flattenTimeSpan(attrs.TimeSpan, model.TimeSpan)

		// The API stores the ID as written (IANA or Windows); empty means UTC.
		if attrs.TimeZoneId != nil && *attrs.TimeZoneId != "" {
			model.TimeZoneID = types.StringValue(*attrs.TimeZoneId)
		} else {
			model.TimeZoneID = types.StringNull()
		}

		model.ExcludeFromDowntime = maintenanceBoolValue(attrs.ExcludeFromDowntime)
		model.ApplyToComponentStatus = maintenanceBoolValue(attrs.ApplyToComponentStatus)
		model.Squelch = maintenanceBoolValue(attrs.Squelch)
		model.CreateDateTime = timePtrToString(attrs.CreateDateTime)
		model.UpdateDateTime = timePtrToString(attrs.UpdateDateTime)
	}

	var (
		ids     []string
		present bool
	)
	if rels := data.Relationships; rels != nil && rels.Components != nil && rels.Components.Data != nil {
		present = true
		for _, item := range *rels.Components.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d := policyRelFlattenIDList(ctx, model.ComponentIDs, ids, present)
	diags.Append(d...)
	model.ComponentIDs = list

	return diags
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

type maintenancePositiveTimeSpanValidator struct{}

// maintenancePositiveTimeSpan validates that a string is a positive .NET TimeSpan.
func maintenancePositiveTimeSpan() maintenancePositiveTimeSpanValidator {
	return maintenancePositiveTimeSpanValidator{}
}

func (v maintenancePositiveTimeSpanValidator) Description(_ context.Context) string {
	return "value must be a positive duration in the form [d.]hh:mm:ss[.fffffff], e.g. 02:00:00"
}

func (v maintenancePositiveTimeSpanValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v maintenancePositiveTimeSpanValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	if d, ok := parseTimeSpan(req.ConfigValue.ValueString()); !ok || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not valid: %s.", req.ConfigValue.ValueString(), v.Description(ctx)))
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// MaintenanceScheduleResourceModelForTest is a type alias for
// maintenanceScheduleResourceModel for use in unit tests.
type MaintenanceScheduleResourceModelForTest = maintenanceScheduleResourceModel

// FlattenMaintenanceScheduleForTest exposes flattenMaintenanceSchedule for use in unit tests.
func FlattenMaintenanceScheduleForTest(ctx context.Context, data api.DataInMaintenanceScheduleResponse, model *maintenanceScheduleResourceModel) diag.Diagnostics {
	return flattenMaintenanceSchedule(ctx, data, model)
}

// BuildCreateMaintenanceScheduleAttributesForTest exposes buildCreateMaintenanceScheduleAttributes for use in unit tests.
func BuildCreateMaintenanceScheduleAttributesForTest(plan maintenanceScheduleResourceModel) *api.AttributesInCreateMaintenanceScheduleRequest {
	return buildCreateMaintenanceScheduleAttributes(plan)
}

// BuildUpdateMaintenanceScheduleAttributesForTest exposes buildUpdateMaintenanceScheduleAttributes for use in unit tests.
func BuildUpdateMaintenanceScheduleAttributesForTest(plan maintenanceScheduleResourceModel) *api.AttributesInUpdateMaintenanceScheduleRequest {
	return buildUpdateMaintenanceScheduleAttributes(plan)
}

// BuildMaintenanceScheduleComponentsForTest exposes buildMaintenanceScheduleComponents for use in unit tests.
func BuildMaintenanceScheduleComponentsForTest(ctx context.Context, plan maintenanceScheduleResourceModel) (*api.ToManyComponentInRequest, diag.Diagnostics) {
	return buildMaintenanceScheduleComponents(ctx, plan)
}

// MaintenanceScheduleNullAttributesForTest exposes maintenanceScheduleNullAttributes for use in unit tests.
func MaintenanceScheduleNullAttributesForTest(plan maintenanceScheduleResourceModel) []string {
	return maintenanceScheduleNullAttributes(plan)
}

// MaintenanceScheduleReadParamsForTest exposes maintenanceScheduleReadParams for use in unit tests.
func MaintenanceScheduleReadParamsForTest() *api.GetMaintenanceScheduleParams {
	return maintenanceScheduleReadParams()
}

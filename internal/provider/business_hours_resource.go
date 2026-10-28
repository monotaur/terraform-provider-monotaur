package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	_ resource.Resource                = &businessHoursResource{}
	_ resource.ResourceWithConfigure   = &businessHoursResource{}
	_ resource.ResourceWithImportState = &businessHoursResource{}
)

// businessHoursMaxNameLength is the server's [MaxLength] on BusinessHours.Name.
const businessHoursMaxNameLength = 250

// businessHoursDays lists the individual days of the API's DaysOfWeek flags
// enum, in flag order (Sunday = 1 … Saturday = 64). These are the only values
// days_of_week accepts: the composite names (Weekdays, Weekends, Everyday) are
// expanded when read so that a set of single days round-trips exactly.
var businessHoursDays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// businessHoursCompositeDays maps the composite flag names the API may return
// (a .NET [Flags] enum renders matching combinations by their composite name,
// e.g. Monday–Friday as "Weekdays") onto their individual days.
var businessHoursCompositeDays = map[string][]string{
	"Weekdays": {"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"},
	"Weekends": {"Sunday", "Saturday"},
	"Everyday": businessHoursDays,
}

// NewBusinessHoursResource returns a new businessHoursResource constructor function.
func NewBusinessHoursResource() resource.Resource {
	return &businessHoursResource{}
}

// businessHoursResource implements the monotaur_business_hours managed resource.
type businessHoursResource struct {
	client *client.Client
}

// businessHoursResourceModel is the Terraform state model for business hours.
//
// Attribute mapping:
//
//	AttributesInCreateBusinessHourRequest: name (required), daysOfWeek, startTime, endTime, timeZoneId
//	AttributesInBusinessHourResponse:      + createDateTime, updateDateTime (computed)
//	RelationshipsIn*BusinessHourRequest:   components — deliberately not managed here; the link is
//	                                       owned by monotaur_component.business_hours_id (and is
//	                                       read-only on this side of the API anyway).
type businessHoursResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	DaysOfWeek     types.Set    `tfsdk:"days_of_week"`
	StartTime      types.String `tfsdk:"start_time"`
	EndTime        types.String `tfsdk:"end_time"`
	TimeZoneID     types.String `tfsdk:"time_zone_id"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
}

// Metadata sets the resource type name.
func (r *businessHoursResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_business_hours"
}

// Schema defines the Terraform schema for the resource.
func (r *businessHoursResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur business-hours schedule: the days and local time range during which a " +
			"component is expected to be operational. The uptime endpoints count only time inside the business hours " +
			"of a component (time outside counts neither for nor against uptime); the `monotaur_uptime_ratio` gauge " +
			"does not apply them. Attach a schedule to a component with `monotaur_component.business_hours_id`; " +
			"deleting the schedule detaches it from its components.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the business hours (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the business hours (1 to 250 characters, at least one letter). Must be unique.",
				Required:            true,
				Validators: []validator.String{
					ncStringLengthBetween(1, businessHoursMaxNameLength),
				},
			},
			"days_of_week": schema.SetAttribute{
				MarkdownDescription: "The days the time range applies to, as individual day names: any of `Sunday`, " +
					"`Monday`, `Tuesday`, `Wednesday`, `Thursday`, `Friday`, `Saturday` (at least one). " +
					"With an overnight range, the day is the one the range starts on.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					ncSetSizeAtLeast(1),
					ncSetElementsOneOf(businessHoursDays...),
				},
			},
			"start_time": schema.StringAttribute{
				MarkdownDescription: "When the range starts each day, as a .NET TimeSpan offset into the local day " +
					"(`hh:mm:ss`, from `00:00:00` up to but excluding 24 hours), e.g. `09:00:00`.",
				Required: true,
				Validators: []validator.String{
					businessHoursTimeSpan(false),
				},
			},
			"end_time": schema.StringAttribute{
				MarkdownDescription: "When the range ends, as a .NET TimeSpan (`hh:mm:ss`, greater than `00:00:00`; " +
					"`1.00:00:00` means midnight at the end of the day, `24:00:00` is not accepted). Must differ from " +
					"`start_time`; an `end_time` earlier than `start_time` is an overnight range that ends on the following day.",
				Required: true,
				Validators: []validator.String{
					businessHoursTimeSpan(true),
				},
			},
			"time_zone_id": schema.StringAttribute{
				MarkdownDescription: "The IANA (e.g. `Europe/Berlin`) or Windows (e.g. `W. Europe Standard Time`) time zone " +
					"whose wall clock the times follow, including daylight-saving changes. Stored as written. When unset, " +
					"times are UTC; removing it from the configuration clears it.",
				Optional: true,
				Validators: []validator.String{
					ncStringLengthBetween(1, 200),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the business hours were created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the business hours were last updated (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *businessHoursResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates new business hours via POST /businessHours.
func (r *businessHoursResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan businessHoursResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildCreateBusinessHoursBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_business_hours: creating business hours", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostBusinessHourWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostBusinessHourParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Business Hours", "Could not create business hours: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Business Hours", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInBusinessHourResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenBusinessHours(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_business_hours: created business hours", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *businessHoursResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state businessHoursResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_business_hours: reading business hours", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetBusinessHour(ctx, id, &api.GetBusinessHourParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours", "Could not read business hours "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the business hours were deleted out-of-band. Remove them
		// from state so the next plan re-creates them.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_business_hours: business hours not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Business Hours", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInBusinessHourResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenBusinessHours(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies existing business hours via PATCH /businessHours/{id}.
func (r *businessHoursResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan businessHoursResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state businessHoursResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	doc, nulls, diags := buildUpdateBusinessHoursBody(ctx, id, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqBody, err := client.MarshalWithNullAttributes(doc, nulls)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Business Hours", "Could not encode request: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_business_hours: updating business hours", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchBusinessHourWithBody(ctx, id, &api.PatchBusinessHourParams{}, client.ContentType, reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Business Hours", "Could not update business hours "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Business Hours", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetBusinessHour(ctx, id, &api.GetBusinessHourParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInBusinessHourResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Business Hours Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenBusinessHours(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_business_hours: updated business hours", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes business hours via DELETE /businessHours/{id}. Components that
// used them are detached by the server (their business hours become null).
func (r *businessHoursResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state businessHoursResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_business_hours: deleting business hours", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteBusinessHour(ctx, id, &api.DeleteBusinessHourParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Business Hours", "Could not delete business hours "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means they are already gone. Treat as a no-op so destroy stays idempotent.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_business_hours: business hours already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Business Hours", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_business_hours: deleted business hours", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_business_hours.example <id>`.
func (r *businessHoursResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// buildCreateBusinessHoursBody converts the plan into the API create document.
func buildCreateBusinessHoursBody(ctx context.Context, plan businessHoursResourceModel) (api.CreateBusinessHourRequestDocument, diag.Diagnostics) {
	attrs := &api.AttributesInCreateBusinessHourRequest{
		OpenapiDiscriminator: api.ResourceTypeBusinessHours,
		Name:                 plan.Name.ValueString(),
	}
	days, diags := expandBusinessHoursDays(ctx, plan.DaysOfWeek)
	attrs.DaysOfWeek = days
	attrs.StartTime = bhKnownStringPtr(plan.StartTime)
	attrs.EndTime = bhKnownStringPtr(plan.EndTime)
	attrs.TimeZoneId = bhKnownStringPtr(plan.TimeZoneID)

	return api.CreateBusinessHourRequestDocument{
		Data: api.DataInCreateBusinessHourRequest{
			Type:       api.ResourceTypeBusinessHours,
			Attributes: attrs,
		},
	}, diags
}

// buildUpdateBusinessHoursBody converts the plan into the API update document
// and lists the attributes to send as explicit JSON nulls: a time_zone_id
// removed from the configuration is cleared rather than left unchanged.
func buildUpdateBusinessHoursBody(ctx context.Context, id string, plan businessHoursResourceModel) (api.UpdateBusinessHourRequestDocument, []string, diag.Diagnostics) {
	attrs := &api.AttributesInUpdateBusinessHourRequest{
		OpenapiDiscriminator: api.ResourceTypeBusinessHours,
	}
	attrs.Name = bhKnownStringPtr(plan.Name)
	days, diags := expandBusinessHoursDays(ctx, plan.DaysOfWeek)
	attrs.DaysOfWeek = days
	attrs.StartTime = bhKnownStringPtr(plan.StartTime)
	attrs.EndTime = bhKnownStringPtr(plan.EndTime)
	attrs.TimeZoneId = bhKnownStringPtr(plan.TimeZoneID)

	var nulls []string
	if plan.TimeZoneID.IsNull() {
		nulls = append(nulls, "timeZoneId")
	}

	return api.UpdateBusinessHourRequestDocument{
		Data: api.DataInUpdateBusinessHourRequest{
			Id:         id,
			Type:       api.ResourceTypeBusinessHours,
			Attributes: attrs,
		},
	}, nulls, diags
}

// bhKnownStringPtr returns a pointer to v's value, or nil when v is null or unknown.
func bhKnownStringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// expandBusinessHoursDays converts the days_of_week set into the API's flags value: the
// day names joined with ", " in flag order (e.g. "Monday, Wednesday"), which is
// how the server reads a .NET [Flags] enum. It returns nil when the set is null
// or unknown.
func expandBusinessHoursDays(ctx context.Context, set types.Set) (*api.DaysOfWeek, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	var names []string
	diags := set.ElementsAs(ctx, &names, false)
	if diags.HasError() {
		return nil, diags
	}
	var ordered []string
	for _, day := range businessHoursDays {
		if slices.Contains(names, day) {
			ordered = append(ordered, day)
		}
	}
	if len(ordered) == 0 {
		return nil, diags
	}
	v := api.DaysOfWeek(strings.Join(ordered, ", "))
	return &v, diags
}

// flattenBusinessHoursDays converts the API's flags value (e.g. "Weekdays, Saturday")
// into a set of individual day names. Composite names are expanded; unknown
// names are kept as-is so they surface as a diff rather than vanish.
func flattenBusinessHoursDays(v *api.DaysOfWeek) (types.Set, diag.Diagnostics) {
	if v == nil || strings.TrimSpace(string(*v)) == "" {
		return types.SetNull(types.StringType), nil
	}
	seen := map[string]bool{}
	var extra []string
	for _, part := range strings.Split(string(*v), ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if days, ok := businessHoursCompositeDays[name]; ok {
			for _, d := range days {
				seen[d] = true
			}
			continue
		}
		if slices.Contains(businessHoursDays, name) {
			seen[name] = true
			continue
		}
		if !seen[name] {
			seen[name] = true
			extra = append(extra, name)
		}
	}
	var out []string
	for _, d := range businessHoursDays {
		if seen[d] {
			out = append(out, d)
		}
	}
	out = append(out, extra...)
	return types.SetValueFrom(context.Background(), types.StringType, out)
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenBusinessHours maps the API DataInBusinessHourResponse onto the Terraform state model.
func flattenBusinessHours(_ context.Context, data api.DataInBusinessHourResponse, model *businessHoursResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	attrs := data.Attributes
	if attrs == nil {
		attrs = &api.AttributesInBusinessHourResponse{}
	}

	if attrs.Name != nil {
		model.Name = types.StringValue(*attrs.Name)
	} else {
		model.Name = types.StringNull()
	}

	days, d := flattenBusinessHoursDays(attrs.DaysOfWeek)
	diags.Append(d...)
	model.DaysOfWeek = days

	model.StartTime = flattenTimeSpan(attrs.StartTime, model.StartTime)
	model.EndTime = flattenTimeSpan(attrs.EndTime, model.EndTime)

	// An empty time zone means UTC to the server, the same as null.
	if attrs.TimeZoneId != nil && *attrs.TimeZoneId != "" {
		model.TimeZoneID = types.StringValue(*attrs.TimeZoneId)
	} else {
		model.TimeZoneID = types.StringNull()
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

	return diags
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

// businessHoursTimeSpanValidator checks that a value is a .NET TimeSpan the
// server accepts for a business-hours bound: hours 0–23 (the server cannot
// parse "24:00:00"), minutes and seconds 0–59, and within the day: a start in
// [00:00:00, 24h), an end in (00:00:00, 24h] (written "1.00:00:00").
type businessHoursTimeSpanValidator struct{ end bool }

func businessHoursTimeSpan(end bool) businessHoursTimeSpanValidator {
	return businessHoursTimeSpanValidator{end: end}
}

func (v businessHoursTimeSpanValidator) Description(_ context.Context) string {
	if v.end {
		return "value must be a TimeSpan greater than 00:00:00 and at most 1.00:00:00, e.g. 17:30:00"
	}
	return "value must be a TimeSpan from 00:00:00 up to but excluding 1.00:00:00, e.g. 09:00:00"
}

func (v businessHoursTimeSpanValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v businessHoursTimeSpanValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	s := req.ConfigValue.ValueString()
	m := timeSpanPattern.FindStringSubmatch(s)
	d, ok := parseTimeSpan(s)
	valid := m != nil && ok && m[1] == "" &&
		len(m[3]) > 0 && bhAtoi(m[3]) < 24 && bhAtoi(m[4]) < 60 && bhAtoi(m[5]) < 60
	if valid {
		if v.end {
			valid = d > 0 && d <= 24*time.Hour
		} else {
			valid = d >= 0 && d < 24*time.Hour
		}
	}
	if !valid {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not allowed: %s.", s, v.Description(ctx)))
	}
}

// bhAtoi parses a run of ASCII digits, returning 0 for an empty string.
func bhAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// BusinessHoursResourceModelForTest is a type alias for businessHoursResourceModel for use in unit tests.
type BusinessHoursResourceModelForTest = businessHoursResourceModel

// FlattenBusinessHoursForTest exposes flattenBusinessHours for use in unit tests.
func FlattenBusinessHoursForTest(ctx context.Context, data api.DataInBusinessHourResponse, model *businessHoursResourceModel) diag.Diagnostics {
	return flattenBusinessHours(ctx, data, model)
}

// BuildCreateBusinessHoursBodyForTest exposes buildCreateBusinessHoursBody for use in unit tests.
func BuildCreateBusinessHoursBodyForTest(ctx context.Context, plan businessHoursResourceModel) (api.CreateBusinessHourRequestDocument, diag.Diagnostics) {
	return buildCreateBusinessHoursBody(ctx, plan)
}

// BuildUpdateBusinessHoursBodyForTest exposes buildUpdateBusinessHoursBody for use in unit tests.
func BuildUpdateBusinessHoursBodyForTest(ctx context.Context, id string, plan businessHoursResourceModel) (api.UpdateBusinessHourRequestDocument, []string, diag.Diagnostics) {
	return buildUpdateBusinessHoursBody(ctx, id, plan)
}

// BusinessHoursTimeSpanValidatorForTest exposes the start_time (end=false) and
// end_time (end=true) validator for use in unit tests.
func BusinessHoursTimeSpanValidatorForTest(end bool) validator.String {
	return businessHoursTimeSpan(end)
}

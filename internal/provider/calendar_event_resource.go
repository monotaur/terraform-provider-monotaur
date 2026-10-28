package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	_ resource.Resource                   = &calendarEventResource{}
	_ resource.ResourceWithConfigure      = &calendarEventResource{}
	_ resource.ResourceWithImportState    = &calendarEventResource{}
	_ resource.ResourceWithValidateConfig = &calendarEventResource{}
)

// API limits on a calendar event's attributes.
const (
	calendarEventMaxTitleLength             = 250
	calendarEventMaxDescriptionLength       = 1000
	calendarEventMaxExternalReferenceLength = 250
)

// NewCalendarEventResource returns a new calendarEventResource constructor function.
func NewCalendarEventResource() resource.Resource {
	return &calendarEventResource{}
}

// calendarEventResource implements the monotaur_calendar_event managed resource.
type calendarEventResource struct {
	client *client.Client
}

// calendarEventResourceModel is the Terraform state model for a calendar event.
//
// Attribute mapping:
//
//	AttributesIn*CalendarEventRequest:   title (required), description, startDateTime,
//	                                     endDateTime, externalReference
//	AttributesInCalendarEventResponse:   the above plus createDateTime, updateDateTime (computed)
//	RelationshipsIn*CalendarEventRequest: labels (to-many) → label_ids
//
// The event↔label link can also be managed from the other side with
// monotaur_label.calendar_event_ids. Each side only sends its list when it is
// set in the configuration, so the two coexist as long as a link is configured
// on one side only.
type calendarEventResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Title             types.String `tfsdk:"title"`
	Description       types.String `tfsdk:"description"`
	StartDateTime     types.String `tfsdk:"start_date_time"`
	EndDateTime       types.String `tfsdk:"end_date_time"`
	ExternalReference types.String `tfsdk:"external_reference"`
	LabelIDs          types.List   `tfsdk:"label_ids"`
	CreateDateTime    types.String `tfsdk:"create_date_time"`
	UpdateDateTime    types.String `tfsdk:"update_date_time"`
}

// Metadata sets the resource type name.
func (r *calendarEventResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_calendar_event"
}

// Schema defines the Terraform schema for the resource.
func (r *calendarEventResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur calendar event: a time-bounded, labelled annotation such as a deployment, " +
			"release, change or holiday, recorded to give context to status history. Calendar events do not affect " +
			"monitoring, alarms or uptime; for planned downtime use `monotaur_maintenance_window`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the calendar event (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The title of the event (1 to 250 characters, at least one letter). Need not be unique.",
				Required:            true,
				Validators: []validator.String{
					ncStringLengthBetween(1, calendarEventMaxTitleLength),
					calendarEventContainsLetter(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A longer description of the event (at most 1000 characters). Removing it from the configuration clears it.",
				Optional:            true,
				Validators: []validator.String{
					ncStringLengthBetween(0, calendarEventMaxDescriptionLength),
				},
			},
			"start_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the event starts, e.g. `2030-01-01T22:00:00Z`. " +
					"The API stores it in UTC; any offset may be used and is kept as written while the instant is unchanged.",
				Required: true,
				Validators: []validator.String{
					maintenanceRFC3339(),
				},
			},
			"end_date_time": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the event ends. Must not be before `start_date_time`; " +
					"equal to it for a point-in-time event.",
				Required: true,
				Validators: []validator.String{
					maintenanceRFC3339(),
				},
			},
			"external_reference": schema.StringAttribute{
				MarkdownDescription: "A reference to an external record such as a ticket, change request or release " +
					"(at most 250 characters, not required to be unique). Removing it from the configuration clears it.",
				Optional: true,
				Validators: []validator.String{
					ncStringLengthBetween(0, calendarEventMaxExternalReferenceLength),
				},
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels attached to the event. Set to `[]` to remove all; leave unset to " +
					"leave the links unmanaged. Do not also manage the same links with `monotaur_label.calendar_event_ids`. " +
					"Deleting a label removes it from its events.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the event was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the event was last updated (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// ValidateConfig rejects an event that ends before it starts. The API only
// checks this when both timestamps are in the request, so it is enforced here
// at plan time as well (and both are always sent).
func (r *calendarEventResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
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
		resp.Diagnostics.AddAttributeError(path.Root("end_date_time"), "Invalid Calendar Event",
			fmt.Sprintf("end_date_time (%s) must not be before start_date_time (%s).", end.ValueString(), start.ValueString()))
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *calendarEventResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new calendar event via POST /calendarEvents.
func (r *calendarEventResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan calendarEventResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := buildCreateCalendarEventBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_calendar_event: creating calendar event", map[string]any{"title": plan.Title.ValueString()})

	params := &api.PostCalendarEventParams{Query: calendarEventIncludeQuery()}
	apiResp, err := r.client.Inner().PostCalendarEventWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, params, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Calendar Event", "Could not create calendar event: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Calendar Event", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInCalendarEventResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenCalendarEvent(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_calendar_event: created calendar event", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *calendarEventResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state calendarEventResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_calendar_event: reading calendar event", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetCalendarEvent(ctx, id, calendarEventReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event", "Could not read calendar event "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the event was deleted out-of-band — remove it from state so
		// the next plan re-creates it instead of erroring on a missing resource.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_calendar_event: calendar event not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Calendar Event", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInCalendarEventResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenCalendarEvent(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing calendar event via PATCH /calendarEvents/{id}.
func (r *calendarEventResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config calendarEventResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	doc, nulls, diags := buildUpdateCalendarEventBody(ctx, id, calendarEventConfiguredLabels(plan, config))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The generated attribute struct omits nil pointers, which JSON:API reads
	// as "no change". Send explicit nulls for nullable attributes the
	// configuration no longer sets so that removing them clears them.
	reqBody, err := client.MarshalWithNullAttributes(doc, nulls)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Calendar Event", "Could not encode request: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_calendar_event: updating calendar event", map[string]any{"id": id})

	params := &api.PatchCalendarEventParams{Query: calendarEventIncludeQuery()}
	apiResp, err := r.client.Inner().PatchCalendarEventWithBody(ctx, id, params, client.ContentType, reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Calendar Event", "Could not update calendar event "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Calendar Event", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetCalendarEvent(ctx, id, calendarEventReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInCalendarEventResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Calendar Event Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenCalendarEvent(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_calendar_event: updated calendar event", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a calendar event via DELETE /calendarEvents/{id}. Its label
// links are removed with it; the labels themselves are kept.
func (r *calendarEventResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state calendarEventResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_calendar_event: deleting calendar event", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteCalendarEvent(ctx, id, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Calendar Event", "Could not delete calendar event "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_calendar_event: calendar event already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Calendar Event", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_calendar_event: deleted calendar event", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_calendar_event.example <id>`.
func (r *calendarEventResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// calendarEventIncludeQuery requests the labels relationship via `?include=`
// so responses carry its `data` (a bare response echoes `links` only, which
// would leave imported events without IDs and hide drift). Included to-many
// collections are paginated by the API (default 10), so it is scoped to the
// maximum page size.
func calendarEventIncludeQuery() *map[string]*string {
	include := "labels"
	pageSize := "labels:500"
	return &map[string]*string{"include": &include, "page[size]": &pageSize}
}

// calendarEventReadParams returns the GET parameters used by Read, the data
// source and the post-update re-fetch.
func calendarEventReadParams() *api.GetCalendarEventParams {
	return &api.GetCalendarEventParams{Query: calendarEventIncludeQuery()}
}

// calendarEventConfiguredLabels returns plan with label_ids set to null when
// the configuration leaves it out, so the update request omits it. An
// unconfigured Optional+Computed list plans as its prior state, and sending it
// would replace links made with monotaur_label.calendar_event_ids.
func calendarEventConfiguredLabels(plan, config calendarEventResourceModel) calendarEventResourceModel {
	if config.LabelIDs.IsNull() {
		plan.LabelIDs = types.ListNull(types.StringType)
	}
	return plan
}

// calendarEventLabels converts a label_ids list into the request payload. It
// returns nil when the list is null or unknown ("leave unchanged"); an empty
// list is sent as `"data": []`, which clears the links.
func calendarEventLabels(ctx context.Context, list types.List) (*api.ToManyLabelInRequest, diag.Diagnostics) {
	ids, ok, diags := policyRelKnownIDs(ctx, list)
	if !ok {
		return nil, diags
	}
	data := make([]api.LabelIdentifierInRequest, len(ids))
	for i, id := range ids {
		data[i] = api.LabelIdentifierInRequest{Id: id, Type: api.ResourceTypeLabels}
	}
	return &api.ToManyLabelInRequest{Data: data}, diags
}

// buildCreateCalendarEventBody converts the plan into the API create document.
func buildCreateCalendarEventBody(ctx context.Context, plan calendarEventResourceModel) (api.CreateCalendarEventRequestDocument, diag.Diagnostics) {
	var diags diag.Diagnostics
	start, d := parseMaintenanceTimestamp("start_date_time", plan.StartDateTime)
	diags.Append(d...)
	end, d := parseMaintenanceTimestamp("end_date_time", plan.EndDateTime)
	diags.Append(d...)
	labels, d := calendarEventLabels(ctx, plan.LabelIDs)
	diags.Append(d...)

	return api.CreateCalendarEventRequestDocument{
		Data: api.DataInCreateCalendarEventRequest{
			Type: api.ResourceTypeCalendarEvents,
			Attributes: &api.AttributesInCreateCalendarEventRequest{
				OpenapiDiscriminator: api.ResourceTypeCalendarEvents,
				Title:                plan.Title.ValueString(),
				Description:          bhKnownStringPtr(plan.Description),
				StartDateTime:        start,
				EndDateTime:          end,
				ExternalReference:    bhKnownStringPtr(plan.ExternalReference),
			},
			Relationships: &api.RelationshipsInCreateCalendarEventRequest{
				OpenapiDiscriminator: api.ResourceTypeCalendarEvents,
				Labels:               labels,
			},
		},
	}, diags
}

// buildUpdateCalendarEventBody converts the plan into the API update document
// and lists the attributes to send as explicit JSON nulls: a description or
// external_reference removed from the configuration is cleared rather than
// left unchanged. Start and end are always sent together, since the API only
// checks that the end is not before the start when both are in the request.
func buildUpdateCalendarEventBody(ctx context.Context, id string, plan calendarEventResourceModel) (api.UpdateCalendarEventRequestDocument, []string, diag.Diagnostics) {
	var diags diag.Diagnostics
	start, d := parseMaintenanceTimestamp("start_date_time", plan.StartDateTime)
	diags.Append(d...)
	end, d := parseMaintenanceTimestamp("end_date_time", plan.EndDateTime)
	diags.Append(d...)
	labels, d := calendarEventLabels(ctx, plan.LabelIDs)
	diags.Append(d...)

	var nulls []string
	if plan.Description.IsNull() {
		nulls = append(nulls, "description")
	}
	if plan.ExternalReference.IsNull() {
		nulls = append(nulls, "externalReference")
	}

	return api.UpdateCalendarEventRequestDocument{
		Data: api.DataInUpdateCalendarEventRequest{
			Id:   id,
			Type: api.ResourceTypeCalendarEvents,
			Attributes: &api.AttributesInUpdateCalendarEventRequest{
				OpenapiDiscriminator: api.ResourceTypeCalendarEvents,
				Title:                bhKnownStringPtr(plan.Title),
				Description:          bhKnownStringPtr(plan.Description),
				StartDateTime:        start,
				EndDateTime:          end,
				ExternalReference:    bhKnownStringPtr(plan.ExternalReference),
			},
			Relationships: &api.RelationshipsInUpdateCalendarEventRequest{
				OpenapiDiscriminator: api.ResourceTypeCalendarEvents,
				Labels:               labels,
			},
		},
	}, nulls, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenCalendarEvent maps the API DataInCalendarEventResponse onto the Terraform state model.
func flattenCalendarEvent(ctx context.Context, data api.DataInCalendarEventResponse, model *calendarEventResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	attrs := data.Attributes
	if attrs == nil {
		attrs = &api.AttributesInCalendarEventResponse{}
	}
	model.Title = ncStringPtrValue(attrs.Title)
	model.Description = ncStringPtrValue(attrs.Description)
	model.StartDateTime = flattenMaintenanceTimestamp(attrs.StartDateTime, model.StartDateTime)
	model.EndDateTime = flattenMaintenanceTimestamp(attrs.EndDateTime, model.EndDateTime)
	model.ExternalReference = ncStringPtrValue(attrs.ExternalReference)
	model.CreateDateTime = timePtrToString(attrs.CreateDateTime)
	model.UpdateDateTime = timePtrToString(attrs.UpdateDateTime)

	// Every request asks for `include=labels`, so present data is the full
	// member set; links-only keeps the prior value.
	var ids []string
	present := data.Relationships != nil && data.Relationships.Labels != nil && data.Relationships.Labels.Data != nil
	if present {
		for _, item := range *data.Relationships.Labels.Data {
			ids = append(ids, item.Id)
		}
	}
	list, d := policyRelFlattenIDList(ctx, model.LabelIDs, ids, present)
	diags.Append(d...)
	model.LabelIDs = list

	return diags
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

type calendarEventContainsLetterValidator struct{}

// calendarEventContainsLetter validates that a string contains at least one
// ASCII letter, matching the API's `.*[a-zA-Z].*` rule for titles.
func calendarEventContainsLetter() calendarEventContainsLetterValidator {
	return calendarEventContainsLetterValidator{}
}

func (v calendarEventContainsLetterValidator) Description(_ context.Context) string {
	return "value must contain at least one letter (a-z or A-Z)"
}

func (v calendarEventContainsLetterValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v calendarEventContainsLetterValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !ncKnownString(req.ConfigValue) {
		return
	}
	s := req.ConfigValue.ValueString()
	if !strings.ContainsFunc(s, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value",
			fmt.Sprintf("%q is not valid: %s.", s, v.Description(ctx)))
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// CalendarEventResourceModelForTest is a type alias for
// calendarEventResourceModel for use in unit tests.
type CalendarEventResourceModelForTest = calendarEventResourceModel

// FlattenCalendarEventForTest exposes flattenCalendarEvent for use in unit tests.
func FlattenCalendarEventForTest(ctx context.Context, data api.DataInCalendarEventResponse, model *calendarEventResourceModel) diag.Diagnostics {
	return flattenCalendarEvent(ctx, data, model)
}

// BuildCreateCalendarEventBodyForTest exposes buildCreateCalendarEventBody for use in unit tests.
func BuildCreateCalendarEventBodyForTest(ctx context.Context, plan calendarEventResourceModel) (api.CreateCalendarEventRequestDocument, diag.Diagnostics) {
	return buildCreateCalendarEventBody(ctx, plan)
}

// BuildUpdateCalendarEventBodyForTest exposes buildUpdateCalendarEventBody for use in unit tests.
func BuildUpdateCalendarEventBodyForTest(ctx context.Context, id string, plan calendarEventResourceModel) (api.UpdateCalendarEventRequestDocument, []string, diag.Diagnostics) {
	return buildUpdateCalendarEventBody(ctx, id, plan)
}

// CalendarEventConfiguredLabelsForTest exposes calendarEventConfiguredLabels for use in unit tests.
func CalendarEventConfiguredLabelsForTest(plan, config calendarEventResourceModel) calendarEventResourceModel {
	return calendarEventConfiguredLabels(plan, config)
}

// CalendarEventReadParamsForTest exposes calendarEventReadParams for use in unit tests.
func CalendarEventReadParamsForTest() *api.GetCalendarEventParams {
	return calendarEventReadParams()
}

// CalendarEventContainsLetterValidatorForTest exposes calendarEventContainsLetter for use in unit tests.
func CalendarEventContainsLetterValidatorForTest() validator.String {
	return calendarEventContainsLetter()
}

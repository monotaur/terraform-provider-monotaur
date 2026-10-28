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
	_ resource.Resource                = &alarmNoteResource{}
	_ resource.ResourceWithConfigure   = &alarmNoteResource{}
	_ resource.ResourceWithImportState = &alarmNoteResource{}
)

// alarmNoteMaxBodyLength is the server's AlarmNote.BodyMaxLength.
const alarmNoteMaxBodyLength = 10_000

// alarmNoteKinds lists the values of the API's AlarmNoteKind enum.
var alarmNoteKinds = []string{string(api.Investigating), string(api.Mitigating), string(api.Resolved)}

// NewAlarmNoteResource returns a new alarmNoteResource constructor function.
func NewAlarmNoteResource() resource.Resource {
	return &alarmNoteResource{}
}

// alarmNoteResource implements the monotaur_alarm_note managed resource.
type alarmNoteResource struct {
	client *client.Client
}

// alarmNoteResourceModel is the Terraform state model for an alarm note.
//
// Attribute mapping:
//
//	AttributesInCreateAlarmNoteRequest: kind (required), body (required), public (optional, default false)
//	AttributesInAlarmNoteResponse:      + author, createDateTime, updateDateTime (computed)
//	RelationshipsIn*AlarmNoteRequest:   alarm_id (to-one, required on create; the server rejects any
//	                                    change afterwards with 422, so it forces replacement)
type alarmNoteResourceModel struct {
	ID             types.String `tfsdk:"id"`
	AlarmID        types.String `tfsdk:"alarm_id"`
	Kind           types.String `tfsdk:"kind"`
	Body           types.String `tfsdk:"body"`
	Public         types.Bool   `tfsdk:"public"`
	Author         types.String `tfsdk:"author"`
	CreateDateTime types.String `tfsdk:"create_date_time"`
	UpdateDateTime types.String `tfsdk:"update_date_time"`
}

// Metadata sets the resource type name.
func (r *alarmNoteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alarm_note"
}

// Schema defines the Terraform schema for the resource.
func (r *alarmNoteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one entry of a Monotaur alarm's incident timeline: what responders are doing " +
			"(`kind`), in markdown (`body`), and whether the public status page may show it (`public`). " +
			"Creating a note sends the `com.monotaur.alarm.note-added` webhook event (to webhook subscriptions " +
			"that opted into it); editing or deleting a note sends no webhook event. A note can be edited, but it " +
			"belongs to its alarm for life, and it is deleted with the alarm.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the note (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"alarm_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the alarm the note belongs to. The API does not allow moving a note to " +
					"another alarm, so changing it replaces the note (and sends a new `note-added` event). " +
					"Notes are allowed on closed alarms.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "The stage of incident response the note reports: one of `Investigating`, " +
					"`Mitigating`, `Resolved`. A `Resolved` note does not close the alarm.",
				Required: true,
				Validators: []validator.String{
					ncStringOneOf(alarmNoteKinds...),
				},
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "The note text in markdown (1 to 10,000 characters), stored as sent; rendering and " +
					"sanitizing it is up to the client.",
				Required: true,
				Validators: []validator.String{
					ncStringLengthBetween(1, alarmNoteMaxBodyLength),
				},
			},
			"public": schema.BoolAttribute{
				MarkdownDescription: "Whether the public status page may show the note. Defaults to `false` on the server.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"author": schema.StringAttribute{
				MarkdownDescription: "Who wrote the note, set by the server from the credential that created it, e.g. " +
					"`service-account:12`. Edits keep the original author.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the note was created, which is its time on the timeline (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the note was last updated (assigned by the API).",
				Computed:            true,
			},
		},
	}
}

// Configure extracts the *client.Client from the provider data.
func (r *alarmNoteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new note via POST /alarmNotes.
func (r *alarmNoteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alarmNoteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := buildCreateAlarmNoteBody(plan)

	tflog.Debug(ctx, "monotaur_alarm_note: creating alarm note", map[string]any{"alarm_id": plan.AlarmID.ValueString()})

	apiResp, err := r.client.Inner().PostAlarmNoteWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostAlarmNoteParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Alarm Note", "Could not create alarm note: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Alarm Note", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmNoteResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note Response", err.Error())
		return
	}

	flattenAlarmNote(data, &plan)

	tflog.Debug(ctx, "monotaur_alarm_note: created alarm note", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *alarmNoteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alarmNoteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm_note: reading alarm note", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetAlarmNote(ctx, id, alarmNoteReadParams())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note", "Could not read alarm note "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the note is gone: deleted out-of-band, cascade-deleted with
		// its alarm, or hidden because its monitor was soft-deleted. Remove it
		// from state so the next plan re-creates it.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_alarm_note: alarm note not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Alarm Note", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInAlarmNoteResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note Response", err.Error())
		return
	}

	flattenAlarmNote(data, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update edits an existing note via PATCH /alarmNotes/{id}. Only kind, body and
// public are sent; alarm_id forces replacement.
func (r *alarmNoteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan alarmNoteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state alarmNoteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	body := buildUpdateAlarmNoteBody(id, plan)

	tflog.Debug(ctx, "monotaur_alarm_note: updating alarm note", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchAlarmNoteWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchAlarmNoteParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Alarm Note", "Could not update alarm note "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Alarm Note", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetAlarmNote(ctx, id, alarmNoteReadParams())
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInAlarmNoteResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Alarm Note Response", err.Error())
		return
	}

	flattenAlarmNote(data, &plan)

	tflog.Debug(ctx, "monotaur_alarm_note: updated alarm note", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a note via DELETE /alarmNotes/{id}.
func (r *alarmNoteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alarmNoteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_alarm_note: deleting alarm note", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteAlarmNote(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Alarm Note", "Could not delete alarm note "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the note is already gone, typically cascade-deleted with
		// its alarm. Treat as a no-op so destroy stays idempotent.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_alarm_note: alarm note already deleted, ignoring 404", map[string]any{"id": id})
			return
		}
		resp.Diagnostics.AddError("Error Deleting Alarm Note", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_alarm_note: deleted alarm note", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_alarm_note.example <id>`.
func (r *alarmNoteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// alarmNoteReadParams requests the alarm relationship via `?include=` so the
// response carries its `data`; a bare GET echoes `links` only, which would
// leave an imported note without alarm_id.
func alarmNoteReadParams() *api.GetAlarmNoteParams {
	include := "alarm"
	query := map[string]*string{"include": &include}
	return &api.GetAlarmNoteParams{Query: &query}
}

// buildCreateAlarmNoteBody converts the plan into the API create document.
func buildCreateAlarmNoteBody(plan alarmNoteResourceModel) api.CreateAlarmNoteRequestDocument {
	attrs := &api.AttributesInCreateAlarmNoteRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarmNotes,
		Kind:                 api.AlarmNoteKind(plan.Kind.ValueString()),
		Body:                 plan.Body.ValueString(),
	}
	if !plan.Public.IsNull() && !plan.Public.IsUnknown() {
		v := plan.Public.ValueBool()
		attrs.Public = &v
	}
	return api.CreateAlarmNoteRequestDocument{
		Data: api.DataInCreateAlarmNoteRequest{
			Type:       api.ResourceTypeAlarmNotes,
			Attributes: attrs,
			Relationships: &api.RelationshipsInCreateAlarmNoteRequest{
				OpenapiDiscriminator: api.ResourceTypeAlarmNotes,
				Alarm: api.ToOneAlarmInRequest{
					Data: api.AlarmIdentifierInRequest{
						Id:   plan.AlarmID.ValueString(),
						Type: api.ResourceTypeAlarms,
					},
				},
			},
		},
	}
}

// buildUpdateAlarmNoteBody converts the plan into the API update document. The
// alarm relationship is never sent: alarm_id forces replacement.
func buildUpdateAlarmNoteBody(id string, plan alarmNoteResourceModel) api.UpdateAlarmNoteRequestDocument {
	attrs := &api.AttributesInUpdateAlarmNoteRequest{
		OpenapiDiscriminator: api.ResourceTypeAlarmNotes,
	}
	if !plan.Kind.IsNull() && !plan.Kind.IsUnknown() {
		k := api.AlarmNoteKind(plan.Kind.ValueString())
		attrs.Kind = &k
	}
	if !plan.Body.IsNull() && !plan.Body.IsUnknown() {
		b := plan.Body.ValueString()
		attrs.Body = &b
	}
	if !plan.Public.IsNull() && !plan.Public.IsUnknown() {
		v := plan.Public.ValueBool()
		attrs.Public = &v
	}
	return api.UpdateAlarmNoteRequestDocument{
		Data: api.DataInUpdateAlarmNoteRequest{
			Id:         id,
			Type:       api.ResourceTypeAlarmNotes,
			Attributes: attrs,
		},
	}
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenAlarmNote maps the API DataInAlarmNoteResponse onto the Terraform state model.
func flattenAlarmNote(data api.DataInAlarmNoteResponse, model *alarmNoteResourceModel) {
	model.ID = types.StringValue(data.Id)

	attrs := data.Attributes
	if attrs == nil {
		attrs = &api.AttributesInAlarmNoteResponse{}
	}

	if attrs.Kind != nil {
		model.Kind = types.StringValue(string(*attrs.Kind))
	} else {
		model.Kind = types.StringNull()
	}
	if attrs.Body != nil {
		model.Body = types.StringValue(*attrs.Body)
	} else {
		model.Body = types.StringNull()
	}
	if attrs.Public != nil {
		model.Public = types.BoolValue(*attrs.Public)
	} else {
		model.Public = types.BoolNull()
	}
	if attrs.Author != nil {
		model.Author = types.StringValue(*attrs.Author)
	} else {
		model.Author = types.StringNull()
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

	// alarm_id — to-one. Preserve the prior value when the API echoes only
	// `links` (POST and PATCH responses carry no relationship `data`).
	if data.Relationships != nil && data.Relationships.Alarm != nil && data.Relationships.Alarm.Data != nil {
		model.AlarmID = types.StringValue(data.Relationships.Alarm.Data.Id)
	} else if model.AlarmID.IsUnknown() {
		model.AlarmID = types.StringNull()
	}
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// AlarmNoteResourceModelForTest is a type alias for alarmNoteResourceModel for use in unit tests.
type AlarmNoteResourceModelForTest = alarmNoteResourceModel

// FlattenAlarmNoteForTest exposes flattenAlarmNote for use in unit tests.
func FlattenAlarmNoteForTest(_ context.Context, data api.DataInAlarmNoteResponse, model *alarmNoteResourceModel) diag.Diagnostics {
	flattenAlarmNote(data, model)
	return nil
}

// BuildCreateAlarmNoteBodyForTest exposes buildCreateAlarmNoteBody for use in unit tests.
func BuildCreateAlarmNoteBodyForTest(plan alarmNoteResourceModel) api.CreateAlarmNoteRequestDocument {
	return buildCreateAlarmNoteBody(plan)
}

// BuildUpdateAlarmNoteBodyForTest exposes buildUpdateAlarmNoteBody for use in unit tests.
func BuildUpdateAlarmNoteBodyForTest(id string, plan alarmNoteResourceModel) api.UpdateAlarmNoteRequestDocument {
	return buildUpdateAlarmNoteBody(id, plan)
}

// AlarmNoteReadParamsForTest exposes alarmNoteReadParams for use in unit tests.
func AlarmNoteReadParamsForTest() *api.GetAlarmNoteParams {
	return alarmNoteReadParams()
}

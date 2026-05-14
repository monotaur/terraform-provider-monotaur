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
	_ resource.Resource                = &labelResource{}
	_ resource.ResourceWithConfigure   = &labelResource{}
	_ resource.ResourceWithImportState = &labelResource{}
)

// NewLabelResource returns a new labelResource constructor function.
func NewLabelResource() resource.Resource {
	return &labelResource{}
}

// labelResource implements the monotaur_label managed resource.
type labelResource struct {
	client *client.Client
}

// labelResourceModel is the Terraform state model for a label.
//
// Attribute mapping:
//
//	AttributesInCreateLabelRequest:  text (required), color (optional), icon (optional)
//	AttributesInLabelResponse:       id (computed), text, color, icon, name (computed), value (computed),
//	                                  createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*LabelRequest:    calendar_event_ids, component_ids, monitor_ids (all to-many)
type labelResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Text             types.String `tfsdk:"text"`
	Color            types.String `tfsdk:"color"`
	Icon             types.String `tfsdk:"icon"`
	Name             types.String `tfsdk:"name"`
	Value            types.String `tfsdk:"value"`
	CreateDateTime   types.String `tfsdk:"create_date_time"`
	UpdateDateTime   types.String `tfsdk:"update_date_time"`
	CalendarEventIDs types.List   `tfsdk:"calendar_event_ids"`
	ComponentIDs     types.List   `tfsdk:"component_ids"`
	MonitorIDs       types.List   `tfsdk:"monitor_ids"`
}

// Metadata sets the resource type name.
func (r *labelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label"
}

// Schema defines the Terraform schema for the resource.
func (r *labelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur label. Labels are tags that can be attached to components, monitors, and calendar events.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the label (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"text": schema.StringAttribute{
				MarkdownDescription: "The display text of the label.",
				Required:            true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "Optional hex colour code for the label (e.g. `#FF5733`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Optional icon for the label. Must be a single Unicode Extended Pictographic codepoint (an emoji), e.g. `🚩`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The computed machine-readable name of the label (assigned by the API).",
				Computed:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The computed value of the label (assigned by the API).",
				Computed:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the label was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the label was last updated (assigned by the API).",
				Computed:            true,
			},
			"calendar_event_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of calendar events associated with this label.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components associated with this label.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of monitors associated with this label.",
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
func (r *labelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new label via POST /labels.
func (r *labelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan labelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateLabelRequest{
		OpenapiDiscriminator: api.ResourceTypeLabels,
		Text:                 plan.Text.ValueString(),
	}
	if !plan.Color.IsNull() && !plan.Color.IsUnknown() {
		v := plan.Color.ValueString()
		attrs.Color = &v
	}
	if !plan.Icon.IsNull() && !plan.Icon.IsUnknown() {
		v := plan.Icon.ValueString()
		attrs.Icon = &v
	}

	rels, diags := buildCreateLabelRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateLabelRequestDocument{
		Data: api.DataInCreateLabelRequest{
			Type:          api.ResourceTypeLabels,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_label: creating label", map[string]any{"text": plan.Text.ValueString()})

	apiResp, err := r.client.Inner().PostLabelWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostLabelParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Label", "Could not create label: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Label", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInLabelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenLabel(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_label: created label", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *labelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state labelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_label: reading label", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetLabel(ctx, id, &api.GetLabelParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label", "Could not read label "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		// 404 means the label was deleted out-of-band — remove it from state so
		// the next plan re-creates it instead of erroring on a missing resource.
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			tflog.Debug(ctx, "monotaur_label: label not found, removing from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Label", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInLabelResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenLabel(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing label via PATCH /labels/{id}.
func (r *labelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan labelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state labelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	attrs := &api.AttributesInUpdateLabelRequest{
		OpenapiDiscriminator: api.ResourceTypeLabels,
	}
	textVal := plan.Text.ValueString()
	attrs.Text = &textVal

	// Known v0.1 limitation: when `color` or `icon` are removed from the
	// Terraform configuration (set to null), the PATCH body omits them entirely.
	// JSON:API treats an absent field as "no change", so the API will not clear
	// the value. To explicitly clear these attributes you must set them to an
	// empty string in the config. This will be addressed in a future release.
	if !plan.Color.IsNull() && !plan.Color.IsUnknown() {
		v := plan.Color.ValueString()
		attrs.Color = &v
	}
	if !plan.Icon.IsNull() && !plan.Icon.IsUnknown() {
		v := plan.Icon.ValueString()
		attrs.Icon = &v
	}

	rels, diags := buildUpdateLabelRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateLabelRequestDocument{
		Data: api.DataInUpdateLabelRequest{
			Id:            id,
			Type:          api.ResourceTypeLabels,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_label: updating label", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchLabelWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchLabelParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Label", "Could not update label "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Label", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetLabel(ctx, id, &api.GetLabelParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInLabelResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Label Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenLabel(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_label: updated label", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a label via DELETE /labels/{id}.
func (r *labelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state labelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_label: deleting label", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteLabel(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Label", "Could not delete label "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Label", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_label: deleted label", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_label.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *labelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateLabelRelationships converts the plan's relationship ID lists into
// the API request type for create operations.
func buildCreateLabelRelationships(ctx context.Context, plan labelResourceModel) (*api.RelationshipsInCreateLabelRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInCreateLabelRequest{
		OpenapiDiscriminator: api.ResourceTypeLabels,
	}
	var diags diag.Diagnostics

	if !plan.CalendarEventIDs.IsNull() && !plan.CalendarEventIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.CalendarEventIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.CalendarEventIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.CalendarEventIdentifierInRequest{Id: id, Type: api.ResourceTypeCalendarEvents}
			}
			rels.CalendarEvents = &api.ToManyCalendarEventInRequest{Data: data}
		}
	}

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

	if !plan.MonitorIDs.IsNull() && !plan.MonitorIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.MonitorIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.MonitorIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.MonitorIdentifierInRequest{Id: id, Type: api.ResourceTypeMonitors}
			}
			rels.Monitors = &api.ToManyMonitorInRequest{Data: data}
		}
	}

	return rels, diags
}

// buildUpdateLabelRelationships converts the plan's relationship ID lists into
// the API request type for update operations.
func buildUpdateLabelRelationships(ctx context.Context, plan labelResourceModel) (*api.RelationshipsInUpdateLabelRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInUpdateLabelRequest{
		OpenapiDiscriminator: api.ResourceTypeLabels,
	}
	var diags diag.Diagnostics

	if !plan.CalendarEventIDs.IsNull() && !plan.CalendarEventIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.CalendarEventIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.CalendarEventIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.CalendarEventIdentifierInRequest{Id: id, Type: api.ResourceTypeCalendarEvents}
			}
			rels.CalendarEvents = &api.ToManyCalendarEventInRequest{Data: data}
		}
	}

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

	if !plan.MonitorIDs.IsNull() && !plan.MonitorIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.MonitorIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.MonitorIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.MonitorIdentifierInRequest{Id: id, Type: api.ResourceTypeMonitors}
			}
			rels.Monitors = &api.ToManyMonitorInRequest{Data: data}
		}
	}

	return rels, diags
}

// ---------------------------------------------------------------------------
// Flatten helpers
// ---------------------------------------------------------------------------

// flattenLabel maps the API DataInLabelResponse onto the Terraform state model.
func flattenLabel(ctx context.Context, data api.DataInLabelResponse, model *labelResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Text != nil {
			model.Text = types.StringValue(*attrs.Text)
		}
		if attrs.Color != nil {
			model.Color = types.StringValue(*attrs.Color)
		} else {
			model.Color = types.StringNull()
		}
		if attrs.Icon != nil {
			model.Icon = types.StringValue(*attrs.Icon)
		} else {
			model.Icon = types.StringNull()
		}
		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
		}
		if attrs.Value != nil {
			model.Value = types.StringValue(*attrs.Value)
		} else {
			model.Value = types.StringNull()
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

	// Relationships often arrive with only `links` (no embedded `data`). When
	// `data` is absent we cannot tell the current member set, so preserve the
	// prior model value — the plan for create/update, or the state for read —
	// to avoid "element 0 has vanished" inconsistency errors after a
	// successful apply.
	rels := data.Relationships

	if rels != nil && rels.CalendarEvents != nil && rels.CalendarEvents.Data != nil {
		ids := make([]string, len(*rels.CalendarEvents.Data))
		for i, item := range *rels.CalendarEvents.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.CalendarEventIDs = list
	} else if model.CalendarEventIDs.IsNull() || model.CalendarEventIDs.IsUnknown() {
		model.CalendarEventIDs = types.ListValueMust(types.StringType, nil)
	}

	if rels != nil && rels.Components != nil && rels.Components.Data != nil {
		ids := make([]string, len(*rels.Components.Data))
		for i, item := range *rels.Components.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.ComponentIDs = list
	} else if model.ComponentIDs.IsNull() || model.ComponentIDs.IsUnknown() {
		model.ComponentIDs = types.ListValueMust(types.StringType, nil)
	}

	if rels != nil && rels.Monitors != nil && rels.Monitors.Data != nil {
		ids := make([]string, len(*rels.Monitors.Data))
		for i, item := range *rels.Monitors.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.MonitorIDs = list
	} else if model.MonitorIDs.IsNull() || model.MonitorIDs.IsUnknown() {
		model.MonitorIDs = types.ListValueMust(types.StringType, nil)
	}

	return diags
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// stringListToSlice extracts []string from a types.List of strings.
func stringListToSlice(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var elems []types.String
	diags := list.ElementsAs(ctx, &elems, false)
	if diags.HasError() {
		return nil, diags
	}
	result := make([]string, len(elems))
	for i, e := range elems {
		result[i] = e.ValueString()
	}
	return result, diags
}

// ---------------------------------------------------------------------------
// Test exports
// ---------------------------------------------------------------------------

// LabelResourceModelForTest is a type alias for labelResourceModel that allows
// unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type LabelResourceModelForTest = labelResourceModel

// FlattenLabelForTest exposes flattenLabel for use in unit tests.
func FlattenLabelForTest(ctx context.Context, data api.DataInLabelResponse, model *labelResourceModel) diag.Diagnostics {
	return flattenLabel(ctx, data, model)
}

// BuildCreateLabelRelationshipsForTest exposes buildCreateLabelRelationships for use in unit tests.
func BuildCreateLabelRelationshipsForTest(ctx context.Context, plan labelResourceModel) (*api.RelationshipsInCreateLabelRequest, diag.Diagnostics) {
	return buildCreateLabelRelationships(ctx, plan)
}

// BuildUpdateLabelRelationshipsForTest exposes buildUpdateLabelRelationships for use in unit tests.
func BuildUpdateLabelRelationshipsForTest(ctx context.Context, plan labelResourceModel) (*api.RelationshipsInUpdateLabelRequest, diag.Diagnostics) {
	return buildUpdateLabelRelationships(ctx, plan)
}

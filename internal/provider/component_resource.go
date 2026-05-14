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
	_ resource.Resource                = &componentResource{}
	_ resource.ResourceWithConfigure   = &componentResource{}
	_ resource.ResourceWithImportState = &componentResource{}
)

// NewComponentResource returns a new componentResource constructor function.
func NewComponentResource() resource.Resource {
	return &componentResource{}
}

// componentResource implements the monotaur_component managed resource.
type componentResource struct {
	client *client.Client
}

// componentResourceModel is the Terraform state model for a component.
//
// Attribute mapping:
//
//	AttributesInCreateComponentRequest:  name (required)
//	AttributesInComponentResponse:       id (computed), name, createDateTime (computed), updateDateTime (computed)
//	RelationshipsIn*ComponentRequest:    business_hours_id (to-one, optional), label_ids, maintenance_window_ids,
//	                                      monitor_ids (all to-many, optional)
type componentResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	CreateDateTime       types.String `tfsdk:"create_date_time"`
	UpdateDateTime       types.String `tfsdk:"update_date_time"`
	BusinessHoursID      types.String `tfsdk:"business_hours_id"`
	LabelIDs             types.List   `tfsdk:"label_ids"`
	MaintenanceWindowIDs types.List   `tfsdk:"maintenance_window_ids"`
	MonitorIDs           types.List   `tfsdk:"monitor_ids"`
}

// Metadata sets the resource type name.
func (r *componentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_component"
}

// Schema defines the Terraform schema for the resource.
func (r *componentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Monotaur component. Components represent services or systems that can be monitored.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the component (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name of the component.",
				Required:            true,
			},
			"create_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the component was created (assigned by the API).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"update_date_time": schema.StringAttribute{
				MarkdownDescription: "The RFC 3339 timestamp when the component was last updated (assigned by the API).",
				Computed:            true,
			},
			"business_hours_id": schema.StringAttribute{
				MarkdownDescription: "ID of the business hours schedule associated with this component (to-one, optional).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"label_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of labels associated with this component.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"maintenance_window_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of maintenance windows associated with this component.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"monitor_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of monitors associated with this component.",
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
func (r *componentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates a new component via POST /components.
func (r *componentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan componentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &api.AttributesInCreateComponentRequest{
		OpenapiDiscriminator: api.ResourceTypeComponents,
		Name:                 plan.Name.ValueString(),
	}

	rels, diags := buildCreateComponentRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateComponentRequestDocument{
		Data: api.DataInCreateComponentRequest{
			Type:          api.ResourceTypeComponents,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_component: creating component", map[string]any{"name": plan.Name.ValueString()})

	apiResp, err := r.client.Inner().PostComponentWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, &api.PostComponentParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Component", "Could not create component: "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Creating Component", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInComponentResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenComponent(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_component: created component", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state from the API.
func (r *componentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state componentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_component: reading component", map[string]any{"id": id})

	apiResp, err := r.client.Inner().GetComponent(ctx, id, &api.GetComponentParams{})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component", "Could not read component "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Reading Component", "API returned an error: "+err.Error())
		return
	}

	data, err := client.UnmarshalDocument[api.DataInComponentResponse](apiResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenComponent(ctx, data, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update modifies an existing component via PATCH /components/{id}.
func (r *componentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan componentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state componentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	nameVal := plan.Name.ValueString()
	attrs := &api.AttributesInUpdateComponentRequest{
		OpenapiDiscriminator: api.ResourceTypeComponents,
		Name:                 &nameVal,
	}

	rels, diags := buildUpdateComponentRelationships(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateComponentRequestDocument{
		Data: api.DataInUpdateComponentRequest{
			Id:            id,
			Type:          api.ResourceTypeComponents,
			Attributes:    attrs,
			Relationships: rels,
		},
	}

	tflog.Debug(ctx, "monotaur_component: updating component", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchComponentWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchComponentParams{}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Component", "Could not update component "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Updating Component", "API returned an error: "+err.Error())
		return
	}

	respBody, cleanup, err := client.ReadOrRefetch(apiResp, func() (*http.Response, error) {
		return r.client.Inner().GetComponent(ctx, id, &api.GetComponentParams{})
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component After Update", err.Error())
		return
	}
	defer cleanup()

	data, err := client.UnmarshalDocument[api.DataInComponentResponse](respBody)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Component Response", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenComponent(ctx, data, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "monotaur_component: updated component", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a component via DELETE /components/{id}.
func (r *componentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state componentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "monotaur_component: deleting component", map[string]any{"id": id})

	apiResp, err := r.client.Inner().DeleteComponent(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting Component", "Could not delete component "+id+": "+err.Error())
		return
	}
	defer apiResp.Body.Close()

	if err := client.CheckResponse(apiResp); err != nil {
		resp.Diagnostics.AddError("Error Deleting Component", "API returned an error: "+err.Error())
		return
	}

	tflog.Debug(ctx, "monotaur_component: deleted component", map[string]any{"id": id})
}

// ImportState supports `terraform import monotaur_component.example <id>`.
// ImportStatePassthroughID sets the "id" attribute from the import ID and then
// the framework automatically invokes Read to populate the rest of the state.
func (r *componentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------------------------------------------------------------------------
// Relationship builders
// ---------------------------------------------------------------------------

// buildCreateComponentRelationships converts the plan's relationship ID fields
// into the API request type for create operations.
func buildCreateComponentRelationships(ctx context.Context, plan componentResourceModel) (*api.RelationshipsInCreateComponentRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInCreateComponentRequest{
		OpenapiDiscriminator: api.ResourceTypeComponents,
	}
	var diags diag.Diagnostics

	// business_hours_id — to-one (nullable)
	if !plan.BusinessHoursID.IsNull() && !plan.BusinessHoursID.IsUnknown() {
		id := plan.BusinessHoursID.ValueString()
		rels.BusinessHours = &api.NullableToOneBusinessHourInRequest{
			Data: &api.BusinessHourIdentifierInRequest{
				Id:   id,
				Type: api.ResourceTypeBusinessHours,
			},
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

	// maintenance_window_ids — to-many
	if !plan.MaintenanceWindowIDs.IsNull() && !plan.MaintenanceWindowIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.MaintenanceWindowIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.MaintenanceWindowIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.MaintenanceWindowIdentifierInRequest{Id: id, Type: api.ResourceTypeMaintenanceWindows}
			}
			rels.MaintenanceWindows = &api.ToManyMaintenanceWindowInRequest{Data: data}
		}
	}

	// monitor_ids — to-many
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

// buildUpdateComponentRelationships converts the plan's relationship ID fields
// into the API request type for update operations.
func buildUpdateComponentRelationships(ctx context.Context, plan componentResourceModel) (*api.RelationshipsInUpdateComponentRequest, diag.Diagnostics) {
	rels := &api.RelationshipsInUpdateComponentRequest{
		OpenapiDiscriminator: api.ResourceTypeComponents,
	}
	var diags diag.Diagnostics

	// business_hours_id — to-one (nullable)
	if !plan.BusinessHoursID.IsNull() && !plan.BusinessHoursID.IsUnknown() {
		id := plan.BusinessHoursID.ValueString()
		rels.BusinessHours = &api.NullableToOneBusinessHourInRequest{
			Data: &api.BusinessHourIdentifierInRequest{
				Id:   id,
				Type: api.ResourceTypeBusinessHours,
			},
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

	// maintenance_window_ids — to-many
	if !plan.MaintenanceWindowIDs.IsNull() && !plan.MaintenanceWindowIDs.IsUnknown() {
		ids, d := stringListToSlice(ctx, plan.MaintenanceWindowIDs)
		diags.Append(d...)
		if !d.HasError() {
			data := make([]api.MaintenanceWindowIdentifierInRequest, len(ids))
			for i, id := range ids {
				data[i] = api.MaintenanceWindowIdentifierInRequest{Id: id, Type: api.ResourceTypeMaintenanceWindows}
			}
			rels.MaintenanceWindows = &api.ToManyMaintenanceWindowInRequest{Data: data}
		}
	}

	// monitor_ids — to-many
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

// flattenComponent maps the API DataInComponentResponse onto the Terraform state model.
func flattenComponent(ctx context.Context, data api.DataInComponentResponse, model *componentResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(data.Id)

	if data.Attributes != nil {
		attrs := data.Attributes

		if attrs.Name != nil {
			model.Name = types.StringValue(*attrs.Name)
		} else {
			model.Name = types.StringNull()
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

	// JSON:API responses from the Monotaur API often echo relationships with only
	// `links` (no embedded `data`). When `data` is absent we cannot tell what the
	// current member/target is, so we preserve the prior model value — the plan
	// for create/update, or the state for read. Overwriting with an empty list
	// or null causes "element 0 has vanished" / "was X, but now null"
	// inconsistency errors after a successful apply.

	rels := data.Relationships

	// business_hours_id — to-one (nullable)
	if rels != nil && rels.BusinessHours != nil && rels.BusinessHours.Data != nil {
		model.BusinessHoursID = types.StringValue(rels.BusinessHours.Data.Id)
	} else if model.BusinessHoursID.IsUnknown() {
		model.BusinessHoursID = types.StringNull()
	}

	// label_ids — to-many
	if rels != nil && rels.Labels != nil && rels.Labels.Data != nil {
		ids := make([]string, len(*rels.Labels.Data))
		for i, item := range *rels.Labels.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.LabelIDs = list
	} else if model.LabelIDs.IsNull() || model.LabelIDs.IsUnknown() {
		model.LabelIDs = types.ListValueMust(types.StringType, nil)
	}

	// maintenance_window_ids — to-many
	if rels != nil && rels.MaintenanceWindows != nil && rels.MaintenanceWindows.Data != nil {
		ids := make([]string, len(*rels.MaintenanceWindows.Data))
		for i, item := range *rels.MaintenanceWindows.Data {
			ids[i] = item.Id
		}
		list, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		model.MaintenanceWindowIDs = list
	} else if model.MaintenanceWindowIDs.IsNull() || model.MaintenanceWindowIDs.IsUnknown() {
		model.MaintenanceWindowIDs = types.ListValueMust(types.StringType, nil)
	}

	// monitor_ids — to-many
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
// Test exports
// ---------------------------------------------------------------------------

// ComponentResourceModelForTest is a type alias for componentResourceModel that
// allows unit tests in the provider_test package to use the same struct layout
// without embedding framework internals.
type ComponentResourceModelForTest = componentResourceModel

// FlattenComponentForTest exposes flattenComponent for use in unit tests.
func FlattenComponentForTest(ctx context.Context, data api.DataInComponentResponse, model *componentResourceModel) diag.Diagnostics {
	return flattenComponent(ctx, data, model)
}

// BuildCreateComponentRelationshipsForTest exposes buildCreateComponentRelationships for use in unit tests.
func BuildCreateComponentRelationshipsForTest(ctx context.Context, plan componentResourceModel) (*api.RelationshipsInCreateComponentRequest, diag.Diagnostics) {
	return buildCreateComponentRelationships(ctx, plan)
}

// BuildUpdateComponentRelationshipsForTest exposes buildUpdateComponentRelationships for use in unit tests.
func BuildUpdateComponentRelationshipsForTest(ctx context.Context, plan componentResourceModel) (*api.RelationshipsInUpdateComponentRequest, diag.Diagnostics) {
	return buildUpdateComponentRelationships(ctx, plan)
}

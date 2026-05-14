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
//	AttributesInCreateMonitorRequest:  name (required), type (optional)
//	AttributesInMonitorResponse:       id (computed), name, type, createDateTime (computed),
//	                                    updateDateTime (computed), status (computed),
//	                                    statusMessage (computed), statusExpirationDateTime (computed)
//	RelationshipsIn*MonitorRequest:    component_ids (to-many, optional)
type monitorResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Type                     types.String `tfsdk:"type"`
	CreateDateTime           types.String `tfsdk:"create_date_time"`
	UpdateDateTime           types.String `tfsdk:"update_date_time"`
	Status                   types.String `tfsdk:"status"`
	StatusMessage            types.String `tfsdk:"status_message"`
	StatusExpirationDateTime types.String `tfsdk:"status_expiration_date_time"`
	ComponentIDs             types.List   `tfsdk:"component_ids"`
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
			"component_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of components associated with this monitor.",
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

	rels, diags := buildUpdateMonitorRelationships(ctx, plan)
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

	tflog.Debug(ctx, "monotaur_monitor: updating monitor", map[string]any{"id": id})

	apiResp, err := r.client.Inner().PatchMonitorWithApplicationVndAPIPlusJSONExtOpenapiBody(ctx, id, &api.PatchMonitorParams{}, body)
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

	apiResp, err := r.client.Inner().DeleteMonitor(ctx, id)
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

	return rels, diags
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

// BuildCreateMonitorRelationshipsForTest exposes buildCreateMonitorRelationships for use in unit tests.
func BuildCreateMonitorRelationshipsForTest(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInCreateMonitorRequest, diag.Diagnostics) {
	return buildCreateMonitorRelationships(ctx, plan)
}

// BuildUpdateMonitorRelationshipsForTest exposes buildUpdateMonitorRelationships for use in unit tests.
func BuildUpdateMonitorRelationshipsForTest(ctx context.Context, plan monitorResourceModel) (*api.RelationshipsInUpdateMonitorRequest, diag.Diagnostics) {
	return buildUpdateMonitorRelationships(ctx, plan)
}

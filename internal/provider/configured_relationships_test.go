package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// Relationship lists left out of the configuration must not be sent on
// update: their plan value is the prior state, and sending it would replace
// links made from the other side of the relationship.

func idList(ids ...string) types.List {
	vals := make([]attr.Value, len(ids))
	for i, id := range ids {
		vals[i] = types.StringValue(id)
	}
	return types.ListValueMust(types.StringType, vals)
}

func TestConfiguredComponentRelationships(t *testing.T) {
	plan := provider.ComponentResourceModelForTest{
		LabelIDs:             idList("l1"),
		MaintenanceWindowIDs: idList("w1"),
		MonitorIDs:           idList(),
	}
	config := provider.ComponentResourceModelForTest{
		LabelIDs:             idList("l1"),
		MaintenanceWindowIDs: types.ListNull(types.StringType),
		MonitorIDs:           idList(),
	}
	got := provider.ConfiguredComponentRelationshipsForTest(plan, config)
	if !got.LabelIDs.Equal(idList("l1")) {
		t.Errorf("label_ids = %v, want configured value kept", got.LabelIDs)
	}
	if !got.MaintenanceWindowIDs.IsNull() {
		t.Errorf("maintenance_window_ids = %v, want null (unconfigured)", got.MaintenanceWindowIDs)
	}
	if !got.MonitorIDs.Equal(idList()) {
		t.Errorf("monitor_ids = %v, want configured empty list kept (clears links)", got.MonitorIDs)
	}
}

func TestConfiguredMonitorRelationships(t *testing.T) {
	plan := provider.MonitorResourceModelForTest{ComponentIDs: idList("c1"), LabelIDs: idList("l1")}
	config := provider.MonitorResourceModelForTest{ComponentIDs: types.ListNull(types.StringType), LabelIDs: idList("l1")}
	got := provider.ConfiguredMonitorRelationshipsForTest(plan, config)
	if !got.ComponentIDs.IsNull() {
		t.Errorf("component_ids = %v, want null (unconfigured)", got.ComponentIDs)
	}
	if !got.LabelIDs.Equal(idList("l1")) {
		t.Errorf("label_ids = %v, want configured value kept", got.LabelIDs)
	}
}

func TestConfiguredLabelRelationships(t *testing.T) {
	plan := provider.LabelResourceModelForTest{CalendarEventIDs: idList("e1"), ComponentIDs: idList("c1"), MonitorIDs: idList("m1")}
	config := provider.LabelResourceModelForTest{
		CalendarEventIDs: types.ListNull(types.StringType),
		ComponentIDs:     idList("c1"),
		MonitorIDs:       types.ListNull(types.StringType),
	}
	got := provider.ConfiguredLabelRelationshipsForTest(plan, config)
	if !got.CalendarEventIDs.IsNull() || !got.MonitorIDs.IsNull() {
		t.Errorf("unconfigured lists = %v / %v, want null", got.CalendarEventIDs, got.MonitorIDs)
	}
	if !got.ComponentIDs.Equal(idList("c1")) {
		t.Errorf("component_ids = %v, want configured value kept", got.ComponentIDs)
	}
}

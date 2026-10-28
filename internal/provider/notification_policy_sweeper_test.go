package provider_test

// notification_policy_sweeper_test.go holds the sweepers for
// monotaur_notification_policy and monotaur_notification_escalation_step.
// They are wired into runAllSweepers in sweeper_test.go.
//
// Order: escalation steps → policies, and both before notification channels,
// labels, components and monitors. Deleting a policy cascades to its steps, so
// sweepNotificationEscalationSteps is a belt-and-braces pass; a label or
// component cannot be deleted while a policy names it (409), so policies must
// be swept before labels and components.

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// notificationSweepPageSize lists up to the API's maximum page size (the
// default is 10). `page[size]` is percent-encoded.
const notificationSweepPageSize = "page%5Bsize%5D=500"

// notificationPolicySweepIDs returns the IDs of notification policies whose
// name starts with sweepPrefix, keyed to their names.
func notificationPolicySweepIDs(ctx context.Context, c *sweepClient) (map[string]string, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/notificationPolicies?"+notificationSweepPageSize, &doc); err != nil {
		return nil, fmt.Errorf("listing notification_policies: %w", err)
	}
	ids := make(map[string]string)
	for _, res := range doc.Data {
		if res.Attributes != nil && res.Attributes.Name != nil && strings.HasPrefix(*res.Attributes.Name, sweepPrefix) {
			ids[res.ID] = *res.Attributes.Name
		}
	}
	return ids, nil
}

// sweepNotificationPolicyIDs returns the set of tfe2e notification policy IDs,
// for passing to sweepNotificationEscalationSteps.
func sweepNotificationPolicyIDs(ctx context.Context, c *sweepClient) (map[string]struct{}, error) {
	named, err := notificationPolicySweepIDs(ctx, c)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(named))
	for id := range named {
		ids[id] = struct{}{}
	}
	return ids, nil
}

// sweepNotificationPolicies deletes notification policies whose name starts
// with sweepPrefix. The API cascades the delete to their escalation steps.
func sweepNotificationPolicies(ctx context.Context, c *sweepClient) (int, error) {
	named, err := notificationPolicySweepIDs(ctx, c)
	if err != nil {
		return 0, err
	}

	var deleted int
	for id, name := range named {
		if err := c.delete(ctx, "/notificationPolicies/"+id); err != nil {
			log.Printf("[WARN] sweeper: failed to delete notification_policy %s (%s): %v", name, id, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted notification_policy %s (%s)", name, id)
		deleted++
	}
	return deleted, nil
}

// sweepNotificationEscalationSteps deletes the escalation steps of the
// policies in policyIDs. Steps have no name; they are found through each
// policy's escalationSteps relationship.
func sweepNotificationEscalationSteps(ctx context.Context, c *sweepClient, policyIDs map[string]struct{}) (int, error) {
	var deleted int
	var errs []string
	for policyID := range policyIDs {
		var doc struct {
			Data []resourceID `json:"data"`
		}
		path := "/notificationPolicies/" + policyID + "/relationships/escalationSteps?" + notificationSweepPageSize
		if err := c.get(ctx, path, &doc); err != nil {
			errs = append(errs, fmt.Sprintf("policy %s: %v", policyID, err))
			continue
		}
		for _, step := range doc.Data {
			if err := c.delete(ctx, "/notificationEscalationSteps/"+step.ID); err != nil {
				log.Printf("[WARN] sweeper: failed to delete notification_escalation_step id=%s (policy=%s): %v",
					step.ID, policyID, err)
				continue
			}
			log.Printf("[INFO] sweeper: deleted notification_escalation_step id=%s (policy=%s)", step.ID, policyID)
			deleted++
		}
	}
	if len(errs) > 0 {
		return deleted, fmt.Errorf("listing notification_escalation_steps: %s", strings.Join(errs, "; "))
	}
	return deleted, nil
}

// Package provider_test contains the acceptance test suite, including resource
// sweepers that delete any staging resources whose names begin with "tfe2e-".
//
// Sweepers run automatically before the test suite via TestMain. The standalone
// TestSweepAll test function allows the Makefile to invoke sweepers in isolation:
//
//	make e2e-sweep
//
// Dependency-safe deletion order (deepest first):
//
//	api_key → role_assignment → role → service_account →
//	monitor_status_rule → alarm → variable → secret →
//	probe → sensor → monitor → component → label
package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
)

// sweepPrefix is the resource-name prefix that identifies test resources.
const sweepPrefix = "tfe2e-"

// sweepClient wraps the raw HTTP client used by sweepers so they can share
// authentication / base-URL logic without pulling in the full provider stack.
type sweepClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// newSweepClient creates a sweep client from the standard env vars.
// Returns an error if MONOTAUR_ENDPOINT or MONOTAUR_API_KEY are unset.
func newSweepClient() (*sweepClient, error) {
	endpoint := os.Getenv("MONOTAUR_ENDPOINT")
	if endpoint == "" {
		return nil, fmt.Errorf("MONOTAUR_ENDPOINT must be set to run sweepers")
	}
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("MONOTAUR_API_KEY must be set to run sweepers")
	}
	return &sweepClient{
		baseURL: strings.TrimRight(endpoint, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}, nil
}

const jsonAPIContentType = "application/vnd.api+json; ext=openapi"

// get performs an authenticated GET and decodes the JSON body into dest.
func (c *sweepClient) get(ctx context.Context, path string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", jsonAPIContentType)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

// delete performs an authenticated DELETE and returns any error.
func (c *sweepClient) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", jsonAPIContentType)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	return nil
}

// deleteWithBody performs an authenticated DELETE with a JSON body (used for
// API key deletion via the service-account relationship endpoint).
func (c *sweepClient) deleteWithBody(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", jsonAPIContentType)
	req.Header.Set("Accept", jsonAPIContentType)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d: %s", path, resp.StatusCode, string(b))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Minimal JSON:API collection response helpers
// ---------------------------------------------------------------------------

type resourceID struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type toOneRelationship struct {
	Data *resourceID `json:"data,omitempty"`
}

type genericAttributes struct {
	Name *string `json:"name,omitempty"`
}

type genericRelationships struct {
	Monitor        *toOneRelationship `json:"monitor,omitempty"`
	ServiceAccount *toOneRelationship `json:"serviceAccount,omitempty"`
	Role           *toOneRelationship `json:"role,omitempty"`
}

type genericResource struct {
	ID            string               `json:"id"`
	Attributes    *genericAttributes   `json:"attributes,omitempty"`
	Relationships *genericRelationships `json:"relationships,omitempty"`
}

type collectionDoc struct {
	Data []genericResource `json:"data"`
}

// ---------------------------------------------------------------------------
// Individual sweeper functions
// ---------------------------------------------------------------------------

// sweepLabels deletes labels whose name starts with sweepPrefix.
func sweepLabels(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/labels", &doc); err != nil {
		return 0, fmt.Errorf("listing labels: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/labels/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete label %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted label %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepComponents deletes components whose name starts with sweepPrefix.
func sweepComponents(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/components", &doc); err != nil {
		return 0, fmt.Errorf("listing components: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/components/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete component %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted component %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepMonitors deletes monitors whose name starts with sweepPrefix.
// Returns the set of deleted monitor IDs so child-resource sweepers can use it.
func sweepMonitors(ctx context.Context, c *sweepClient) (int, map[string]struct{}, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/monitors", &doc); err != nil {
		return 0, nil, fmt.Errorf("listing monitors: %w", err)
	}

	matchIDs := make(map[string]struct{})
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if strings.HasPrefix(name, sweepPrefix) {
			matchIDs[res.ID] = struct{}{}
		}
	}

	var deleted int
	for id := range matchIDs {
		if err := c.delete(ctx, "/monitors/"+id); err != nil {
			log.Printf("[WARN] sweeper: failed to delete monitor id=%s: %v", id, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted monitor id=%s", id)
		deleted++
	}
	return deleted, matchIDs, nil
}

// sweepSensors deletes sensors whose name starts with sweepPrefix.
func sweepSensors(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/sensors", &doc); err != nil {
		return 0, fmt.Errorf("listing sensors: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/sensors/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete sensor %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted sensor %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepProbes deletes probes whose parent monitor ID is in monitorIDs.
// Probes have no name of their own; they are identified by their monitor relationship.
func sweepProbes(ctx context.Context, c *sweepClient, monitorIDs map[string]struct{}) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/probes", &doc); err != nil {
		return 0, fmt.Errorf("listing probes: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Relationships == nil || res.Relationships.Monitor == nil ||
			res.Relationships.Monitor.Data == nil {
			continue
		}
		parentMonitorID := res.Relationships.Monitor.Data.ID
		if _, ok := monitorIDs[parentMonitorID]; !ok {
			continue
		}
		if err := c.delete(ctx, "/probes/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete probe id=%s (monitor=%s): %v",
				res.ID, parentMonitorID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted probe id=%s (monitor=%s)", res.ID, parentMonitorID)
		deleted++
	}
	return deleted, nil
}

// sweepMonitorStatusRules deletes monitor_status_rules whose parent monitor is in monitorIDs.
func sweepMonitorStatusRules(ctx context.Context, c *sweepClient, monitorIDs map[string]struct{}) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/monitor-status-rules", &doc); err != nil {
		return 0, fmt.Errorf("listing monitor_status_rules: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Relationships == nil || res.Relationships.Monitor == nil ||
			res.Relationships.Monitor.Data == nil {
			continue
		}
		parentMonitorID := res.Relationships.Monitor.Data.ID
		if _, ok := monitorIDs[parentMonitorID]; !ok {
			continue
		}
		if err := c.delete(ctx, "/monitor-status-rules/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete monitor_status_rule id=%s (monitor=%s): %v",
				res.ID, parentMonitorID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted monitor_status_rule id=%s (monitor=%s)",
			res.ID, parentMonitorID)
		deleted++
	}
	return deleted, nil
}

// sweepAlarms deletes alarms whose parent monitor is in monitorIDs.
func sweepAlarms(ctx context.Context, c *sweepClient, monitorIDs map[string]struct{}) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/alarms", &doc); err != nil {
		return 0, fmt.Errorf("listing alarms: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Relationships == nil || res.Relationships.Monitor == nil ||
			res.Relationships.Monitor.Data == nil {
			continue
		}
		parentMonitorID := res.Relationships.Monitor.Data.ID
		if _, ok := monitorIDs[parentMonitorID]; !ok {
			continue
		}
		if err := c.delete(ctx, "/alarms/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete alarm id=%s (monitor=%s): %v",
				res.ID, parentMonitorID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted alarm id=%s (monitor=%s)", res.ID, parentMonitorID)
		deleted++
	}
	return deleted, nil
}

// sweepVariables deletes variables whose name starts with sweepPrefix.
func sweepVariables(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/variables", &doc); err != nil {
		return 0, fmt.Errorf("listing variables: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/variables/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete variable %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted variable %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepSecrets deletes secrets whose name starts with sweepPrefix.
func sweepSecrets(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/secrets", &doc); err != nil {
		return 0, fmt.Errorf("listing secrets: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/secrets/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete secret %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted secret %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepServiceAccounts deletes service accounts whose name starts with sweepPrefix.
// Returns the IDs of deleted service accounts so api_key and role_assignment
// sweepers can cross-reference them.
func sweepServiceAccounts(ctx context.Context, c *sweepClient) (int, map[string]struct{}, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/admin/service-accounts", &doc); err != nil {
		return 0, nil, fmt.Errorf("listing service_accounts: %w", err)
	}

	matchIDs := make(map[string]struct{})
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if strings.HasPrefix(name, sweepPrefix) {
			matchIDs[res.ID] = struct{}{}
		}
	}

	var deleted int
	for id := range matchIDs {
		if err := c.delete(ctx, "/admin/service-accounts/"+id); err != nil {
			log.Printf("[WARN] sweeper: failed to delete service_account id=%s: %v", id, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted service_account id=%s", id)
		deleted++
	}
	return deleted, matchIDs, nil
}

// sweepRoles deletes roles whose name starts with sweepPrefix.
func sweepRoles(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/admin/roles", &doc); err != nil {
		return 0, fmt.Errorf("listing roles: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}
		if err := c.delete(ctx, "/admin/roles/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete role %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted role %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepRoleAssignments deletes role assignments whose role or service account
// ID is in the provided sets. Role assignments have no name; they are
// identified through their relationships.
func sweepRoleAssignments(ctx context.Context, c *sweepClient, serviceAccountIDs map[string]struct{}) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/admin/role-assignments", &doc); err != nil {
		return 0, fmt.Errorf("listing role_assignments: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		if res.Relationships == nil {
			continue
		}
		match := false
		if res.Relationships.ServiceAccount != nil &&
			res.Relationships.ServiceAccount.Data != nil {
			if _, ok := serviceAccountIDs[res.Relationships.ServiceAccount.Data.ID]; ok {
				match = true
			}
		}
		if !match {
			continue
		}
		if err := c.delete(ctx, "/admin/role-assignments/"+res.ID); err != nil {
			log.Printf("[WARN] sweeper: failed to delete role_assignment id=%s: %v", res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted role_assignment id=%s", res.ID)
		deleted++
	}
	return deleted, nil
}

// sweepAPIKeys deletes API keys whose name starts with sweepPrefix. Because the
// API does not expose DELETE /admin/api-keys/{id}, each key is deleted via its
// owning service account's relationship endpoint.
func sweepAPIKeys(ctx context.Context, c *sweepClient) (int, error) {
	var doc collectionDoc
	if err := c.get(ctx, "/admin/api-keys", &doc); err != nil {
		return 0, fmt.Errorf("listing api_keys: %w", err)
	}

	var deleted int
	for _, res := range doc.Data {
		name := ""
		if res.Attributes != nil && res.Attributes.Name != nil {
			name = *res.Attributes.Name
		}
		if !strings.HasPrefix(name, sweepPrefix) {
			continue
		}

		// Locate the owning service account from the relationship.
		if res.Relationships == nil || res.Relationships.ServiceAccount == nil ||
			res.Relationships.ServiceAccount.Data == nil {
			log.Printf("[WARN] sweeper: api_key %s (%s) has no service account relationship — skipping",
				name, res.ID)
			continue
		}
		saID := res.Relationships.ServiceAccount.Data.ID

		// Build the DELETE relationship body.
		body, err := json.Marshal(map[string]interface{}{
			"data": []map[string]string{
				{"id": res.ID, "type": "admin.apiKeys"},
			},
		})
		if err != nil {
			log.Printf("[WARN] sweeper: failed to marshal api_key deletion body for %s (%s): %v",
				name, res.ID, err)
			continue
		}

		path := fmt.Sprintf("/admin/service-accounts/%s/relationships/api-keys", saID)
		if err := c.deleteWithBody(ctx, path, body); err != nil {
			log.Printf("[WARN] sweeper: failed to delete api_key %s (%s): %v", name, res.ID, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted api_key %s (%s)", name, res.ID)
		deleted++
	}
	return deleted, nil
}

// ---------------------------------------------------------------------------
// runAllSweepers executes all sweepers in dependency order and returns the
// total number of deletions and any fatal error.
// Individual resource failures are non-fatal (logged + continue).
// ---------------------------------------------------------------------------

func runAllSweepers(ctx context.Context) (int, []error) {
	c, err := newSweepClient()
	if err != nil {
		return 0, []error{err}
	}

	var total int
	var errs []error

	logStep := func(resource string, n int, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("sweep %s: %w", resource, err))
			log.Printf("[ERROR] sweeper: %s: %v", resource, err)
		} else {
			total += n
			log.Printf("[INFO] sweeper: %s: deleted %d resource(s)", resource, n)
		}
	}

	// 1. API keys (deepest — depend on service accounts).
	n, err := sweepAPIKeys(ctx, c)
	logStep("api_key", n, err)

	// 2. Role assignments (depend on roles and service accounts).
	//    Collect service account IDs that match tfe2e- for cross-reference.
	var saDoc collectionDoc
	var saMatchIDs map[string]struct{}
	if listErr := c.get(ctx, "/admin/service-accounts", &saDoc); listErr == nil {
		saMatchIDs = make(map[string]struct{})
		for _, res := range saDoc.Data {
			name := ""
			if res.Attributes != nil && res.Attributes.Name != nil {
				name = *res.Attributes.Name
			}
			if strings.HasPrefix(name, sweepPrefix) {
				saMatchIDs[res.ID] = struct{}{}
			}
		}
	} else {
		log.Printf("[WARN] sweeper: could not list service_accounts for role_assignment cross-ref: %v", listErr)
		saMatchIDs = make(map[string]struct{})
	}
	n, err = sweepRoleAssignments(ctx, c, saMatchIDs)
	logStep("role_assignment", n, err)

	// 3. Roles.
	n, err = sweepRoles(ctx, c)
	logStep("role", n, err)

	// 4. Service accounts.
	n, _, err = sweepServiceAccounts(ctx, c)
	logStep("service_account", n, err)

	// 5. Collect tfe2e- monitor IDs for child-resource sweepers.
	var monDoc collectionDoc
	var monMatchIDs map[string]struct{}
	if listErr := c.get(ctx, "/monitors", &monDoc); listErr == nil {
		monMatchIDs = make(map[string]struct{})
		for _, res := range monDoc.Data {
			name := ""
			if res.Attributes != nil && res.Attributes.Name != nil {
				name = *res.Attributes.Name
			}
			if strings.HasPrefix(name, sweepPrefix) {
				monMatchIDs[res.ID] = struct{}{}
			}
		}
	} else {
		log.Printf("[WARN] sweeper: could not list monitors for child cross-ref: %v", listErr)
		monMatchIDs = make(map[string]struct{})
	}

	// 6. Monitor status rules (depend on monitors).
	n, err = sweepMonitorStatusRules(ctx, c, monMatchIDs)
	logStep("monitor_status_rule", n, err)

	// 7. Alarms (depend on monitors).
	n, err = sweepAlarms(ctx, c, monMatchIDs)
	logStep("alarm", n, err)

	// 8. Variables.
	n, err = sweepVariables(ctx, c)
	logStep("variable", n, err)

	// 9. Secrets.
	n, err = sweepSecrets(ctx, c)
	logStep("secret", n, err)

	// 10. Probes (depend on monitors).
	n, err = sweepProbes(ctx, c, monMatchIDs)
	logStep("probe", n, err)

	// 11. Sensors.
	n, err = sweepSensors(ctx, c)
	logStep("sensor", n, err)

	// 12. Monitors.
	n, _, err = sweepMonitors(ctx, c)
	logStep("monitor", n, err)

	// 13. Components.
	n, err = sweepComponents(ctx, c)
	logStep("component", n, err)

	// 14. Labels (shallowest).
	n, err = sweepLabels(ctx, c)
	logStep("label", n, err)

	log.Printf("[INFO] sweeper: total deletions: %d", total)
	return total, errs
}

// ---------------------------------------------------------------------------
// TestMain — run sweepers before the acceptance suite when SWEEP=true.
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	if os.Getenv("SWEEP") == "true" {
		if os.Getenv("MONOTAUR_ENDPOINT") != "" && os.Getenv("MONOTAUR_API_KEY") != "" {
			log.Println("[INFO] sweeper: running pre-suite sweep...")
			ctx := context.Background()
			total, errs := runAllSweepers(ctx)
			if len(errs) > 0 {
				log.Printf("[WARN] sweeper: %d error(s) during pre-suite sweep (non-fatal)", len(errs))
			}
			log.Printf("[INFO] sweeper: pre-suite sweep complete; %d resource(s) deleted", total)
		}
	}
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// TestSweepAll — standalone sweep test invoked by `make e2e-sweep`.
// Exits with a non-zero code if any sweeper returns an error (fatal for the
// standalone target, as required by the acceptance criteria).
// ---------------------------------------------------------------------------

// TestSweepAll runs all sweepers and writes a summary to e2e-results/sweep.txt.
// It is guarded by TF_ACC=1 so it never runs in the regular unit-test pass.
func TestSweepAll(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("skipping sweeper: TF_ACC not set")
	}

	ctx := context.Background()
	total, errs := runAllSweepers(ctx)

	// Write summary to e2e-results/sweep.txt.
	if err := os.MkdirAll("e2e-results", 0o755); err != nil {
		t.Logf("WARNING: could not create e2e-results directory: %v", err)
	} else {
		summary := fmt.Sprintf("sweep complete: %d resource(s) deleted, %d error(s)\n",
			total, len(errs))
		for _, e := range errs {
			summary += fmt.Sprintf("  ERROR: %v\n", e)
		}
		if writeErr := os.WriteFile("e2e-results/sweep.txt", []byte(summary), 0o644); writeErr != nil {
			t.Logf("WARNING: could not write e2e-results/sweep.txt: %v", writeErr)
		}
	}

	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("sweeper error: %v", e)
		}
		t.Fatalf("sweep completed with %d error(s); %d resource(s) deleted", len(errs), total)
	}

	t.Logf("sweep complete: %d resource(s) deleted", total)
}

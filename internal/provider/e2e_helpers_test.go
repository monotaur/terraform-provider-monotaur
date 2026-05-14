package provider_test

// e2e_helpers_test.go contains shared helpers used across multiple E2E test
// files. Functions here require MONOTAUR_ENDPOINT and MONOTAUR_API_KEY and are
// only called from tests already gated on TF_ACC=1.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// createServiceAccount creates a service account via the raw admin API and
// returns its ID along with a cleanup function that deletes it. The caller
// must call the returned cleanup (typically via defer) to avoid leaving test
// resources behind.
//
// Always uses a dedicated http.Client with a 30-second timeout; never uses
// http.DefaultClient.
func createServiceAccount(name string) (id string, cleanup func(), err error) {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return "", nil, fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "admin.serviceAccounts",
			"attributes": map[string]interface{}{
				"name": name,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal create body: %w", err)
	}

	const ct = "application/vnd.api+json; ext=openapi"
	url := fmt.Sprintf("%s/api/v1/admin.serviceAccounts", endpoint)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("execute POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("POST /api/v1/admin.serviceAccounts: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("decode create response: %w", err)
	}
	if result.Data.ID == "" {
		return "", nil, fmt.Errorf("create service account returned empty ID")
	}

	saID := result.Data.ID
	cleanupFn := func() {
		deleteURL := fmt.Sprintf("%s/api/v1/admin.serviceAccounts/%s", endpoint, saID)
		delReq, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "createServiceAccount cleanup: build DELETE request for %s: %v\n", saID, err)
			return
		}
		delReq.Header.Set("Accept", ct)
		delReq.Header.Set("Authorization", "Bearer "+apiKey)

		delClient := &http.Client{Timeout: 30 * time.Second}
		delResp, err := delClient.Do(delReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "createServiceAccount cleanup: DELETE /api/v1/admin.serviceAccounts/%s: %v\n", saID, err)
			return
		}
		defer delResp.Body.Close()
		if delResp.StatusCode < 200 || delResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(delResp.Body)
			fmt.Fprintf(os.Stderr, "createServiceAccount cleanup: DELETE /api/v1/admin.serviceAccounts/%s: HTTP %d: %s\n",
				saID, delResp.StatusCode, string(raw))
		}
	}

	return saID, cleanupFn, nil
}

// rawAPICreate performs a generic JSON:API POST against /api/v1/{collection}
// using the provided attributes and relationships, returning the new resource's
// ID and a cleanup function that DELETEs it via /api/v1/{collection}/{id}.
//
// This underlies the per-resource raw helpers used to break Terraform graph
// cycles in tests (e.g. TestAccMonotaurProbe_sensorIDs, where sensor.probe_id
// and probe.sensor_ids reference each other and cannot coexist as Terraform
// resources in the same config).
func rawAPICreate(
	collection, resourceType string,
	attributes, relationships map[string]interface{},
) (id string, cleanup func(), err error) {
	endpoint := strings.TrimRight(os.Getenv("MONOTAUR_ENDPOINT"), "/")
	apiKey := os.Getenv("MONOTAUR_API_KEY")
	if endpoint == "" || apiKey == "" {
		return "", nil, fmt.Errorf("MONOTAUR_ENDPOINT and MONOTAUR_API_KEY must be set")
	}

	data := map[string]interface{}{
		"type":       resourceType,
		"attributes": attributes,
	}
	if len(relationships) > 0 {
		data["relationships"] = relationships
	}
	body, err := json.Marshal(map[string]interface{}{"data": data})
	if err != nil {
		return "", nil, fmt.Errorf("marshal create body for %s: %w", collection, err)
	}

	const ct = "application/vnd.api+json; ext=openapi"
	url := fmt.Sprintf("%s/api/v1/%s", endpoint, collection)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("build POST %s: %w", collection, err)
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", ct)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("execute POST %s: %w", collection, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("POST /api/v1/%s: HTTP %d: %s", collection, resp.StatusCode, string(raw))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("decode POST %s response: %w", collection, err)
	}
	if result.Data.ID == "" {
		return "", nil, fmt.Errorf("POST /api/v1/%s returned empty ID", collection)
	}

	newID := result.Data.ID
	cleanupFn := func() {
		deleteURL := fmt.Sprintf("%s/api/v1/%s/%s", endpoint, collection, newID)
		delReq, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rawAPICreate cleanup: build DELETE for %s/%s: %v\n", collection, newID, err)
			return
		}
		delReq.Header.Set("Accept", ct)
		delReq.Header.Set("Authorization", "Bearer "+apiKey)

		delClient := &http.Client{Timeout: 30 * time.Second}
		delResp, err := delClient.Do(delReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rawAPICreate cleanup: DELETE /api/v1/%s/%s: %v\n", collection, newID, err)
			return
		}
		defer delResp.Body.Close()
		// 404 is fine — resource may have been cascade-deleted by a parent.
		if delResp.StatusCode == http.StatusNotFound {
			return
		}
		if delResp.StatusCode < 200 || delResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(delResp.Body)
			fmt.Fprintf(os.Stderr, "rawAPICreate cleanup: DELETE /api/v1/%s/%s: HTTP %d: %s\n",
				collection, newID, delResp.StatusCode, string(raw))
		}
	}

	return newID, cleanupFn, nil
}

// toOneRel builds a JSON:API to-one relationship document.
func toOneRel(relType, id string) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{"type": relType, "id": id},
	}
}

// toManyRel builds a JSON:API to-many relationship document.
func toManyRel(relType string, ids []string) map[string]interface{} {
	data := make([]map[string]interface{}, len(ids))
	for i, id := range ids {
		data[i] = map[string]interface{}{"type": relType, "id": id}
	}
	return map[string]interface{}{"data": data}
}

// createLabel creates a label via raw API. Returns its ID and a cleanup func.
func createLabel(text string) (string, func(), error) {
	return rawAPICreate("labels", "labels", map[string]interface{}{
		"openapi:discriminator": "labels",
		"text":                  text,
	}, nil)
}

// createComponent creates a component via raw API, optionally with a label
// to-many relationship. Returns its ID and a cleanup func.
func createComponent(name string, labelIDs []string) (string, func(), error) {
	rels := map[string]interface{}{}
	if len(labelIDs) > 0 {
		rels["labels"] = toManyRel("labels", labelIDs)
	}
	return rawAPICreate("components", "components", map[string]interface{}{
		"openapi:discriminator": "components",
		"name":                  name,
	}, rels)
}

// createMonitor creates a monitor via raw API, optionally with components.
// Returns its ID and a cleanup func.
func createMonitor(name string, componentIDs []string) (string, func(), error) {
	rels := map[string]interface{}{}
	if len(componentIDs) > 0 {
		rels["components"] = toManyRel("components", componentIDs)
	}
	return rawAPICreate("monitors", "monitors", map[string]interface{}{
		"openapi:discriminator": "monitors",
		"name":                  name,
	}, rels)
}

// createProbe creates a probe via raw API, owned by the given monitor.
// Returns its ID and a cleanup func.
func createProbe(monitorID string) (string, func(), error) {
	return rawAPICreate("probes", "probes", map[string]interface{}{
		"openapi:discriminator": "probes",
	}, map[string]interface{}{
		"monitor": toOneRel("monitors", monitorID),
	})
}

// createSensor creates a sensor via raw API, owned by the given probe.
// Returns its ID and a cleanup func.
func createSensor(probeID, name string) (string, func(), error) {
	return rawAPICreate("sensors", "sensors", map[string]interface{}{
		"openapi:discriminator": "sensors",
		"name":                  name,
		"pluginName":            "http",
		"type":                  "HttpSensor",
	}, map[string]interface{}{
		"probe": toOneRel("probes", probeID),
	})
}

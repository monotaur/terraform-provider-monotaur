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
	url := fmt.Sprintf("%s/admin/service-accounts", endpoint)
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
		return "", nil, fmt.Errorf("POST /admin/service-accounts: HTTP %d: %s", resp.StatusCode, string(raw))
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
		deleteURL := fmt.Sprintf("%s/admin/service-accounts/%s", endpoint, saID)
		delReq, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
		if err != nil {
			return
		}
		delReq.Header.Set("Accept", ct)
		delReq.Header.Set("Authorization", "Bearer "+apiKey)

		delClient := &http.Client{Timeout: 30 * time.Second}
		delResp, err := delClient.Do(delReq)
		if err != nil {
			return
		}
		delResp.Body.Close()
	}

	return saID, cleanupFn, nil
}

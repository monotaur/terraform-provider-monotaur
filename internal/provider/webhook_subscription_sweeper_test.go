package provider_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// webhookSubscriptionSweepPageSize is the page size used to list subscriptions
// (the API default is 10; the maximum is 500).
const webhookSubscriptionSweepPageSize = 100

// webhookSubscriptionSweepDoc is a page of GET /webhookSubscriptions, decoding
// only what the sweeper needs.
type webhookSubscriptionSweepDoc struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes *struct {
			URL *string `json:"url,omitempty"`
		} `json:"attributes,omitempty"`
	} `json:"data"`
}

// webhookSubscriptionURLIsTest reports whether a subscription URL was created
// by the E2E tests. Subscriptions have no name, so the tests put an acctest
// name in the URL path (https://example.com/tfe2e-<prefix>-webhook-subscription-<n>/hook);
// a path segment starting with "tfe2e-" marks it.
func webhookSubscriptionURLIsTest(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if strings.HasPrefix(seg, sweepPrefix+"-") {
			return true
		}
	}
	return false
}

// sweepWebhookSubscriptions deletes webhook subscriptions whose URL path has a
// segment starting with sweepPrefix (see webhookSubscriptionURLIsTest).
func sweepWebhookSubscriptions(ctx context.Context, c *sweepClient) (int, error) {
	// Collect every match first: deleting while paging would shift the pages.
	type match struct{ id, url string }
	var matches []match
	for page := 1; ; page++ {
		var doc webhookSubscriptionSweepDoc
		path := fmt.Sprintf("/webhookSubscriptions?page%%5Bsize%%5D=%d&page%%5Bnumber%%5D=%d", webhookSubscriptionSweepPageSize, page)
		if err := c.get(ctx, path, &doc); err != nil {
			return 0, fmt.Errorf("listing webhook_subscriptions: %w", err)
		}
		for _, res := range doc.Data {
			if res.Attributes == nil || res.Attributes.URL == nil {
				continue
			}
			if webhookSubscriptionURLIsTest(*res.Attributes.URL) {
				matches = append(matches, match{id: res.ID, url: *res.Attributes.URL})
			}
		}
		if len(doc.Data) < webhookSubscriptionSweepPageSize {
			break
		}
	}

	var deleted int
	for _, m := range matches {
		if err := c.delete(ctx, "/webhookSubscriptions/"+m.id); err != nil {
			log.Printf("[WARN] sweeper: failed to delete webhook_subscription %s (%s): %v", m.url, m.id, err)
			continue
		}
		log.Printf("[INFO] sweeper: deleted webhook_subscription %s (%s)", m.url, m.id)
		deleted++
	}
	return deleted, nil
}

// TestWebhookSubscriptionSweeper checks matching and paging against a fake API
// (no live endpoint, no TF_ACC).
func TestWebhookSubscriptionSweeper(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com/tfe2e-a3f9c2-webhook-subscription-1/hook": true,
		"https://example.com/a/tfe2e-a3f9c2-x":                         true,
		"https://example.com/hook":                                     false,
		"https://tfe2e-host.example.com/hook":                          false,
		"https://example.com/hook?x=/tfe2e-a":                          false,
		"https://example.com/tfe2ex/hook":                              false,
	} {
		if got := webhookSubscriptionURLIsTest(raw); got != want {
			t.Errorf("webhookSubscriptionURLIsTest(%q) = %v, want %v", raw, got, want)
		}
	}

	// Page 1 is full (only non-test subscriptions), page 2
	// holds two test subscriptions and one other.
	var deletedIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			var items []string
			switch r.URL.Query().Get("page[number]") {
			case "1":
				for i := 0; i < webhookSubscriptionSweepPageSize; i++ {
					items = append(items, fmt.Sprintf(`{"id":"%d","attributes":{"url":"https://example.com/keep"}}`, i))
				}
			case "2":
				items = []string{
					`{"id":"200","attributes":{"url":"https://example.com/tfe2e-a3f9c2-webhook-subscription-1/hook"}}`,
					`{"id":"201","attributes":{"url":"https://example.com/other"}}`,
					`{"id":"202","attributes":{"url":"https://example.com/tfe2e-ffffff-webhook-subscription-2/hook"}}`,
				}
			}
			if r.URL.Query().Get("page[size]") != fmt.Sprint(webhookSubscriptionSweepPageSize) {
				t.Errorf("page[size]: got %q", r.URL.Query().Get("page[size]"))
			}
			_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(items, ","))
		case http.MethodDelete:
			deletedIDs = append(deletedIDs, strings.TrimPrefix(r.URL.Path, "/webhookSubscriptions/"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := &sweepClient{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
	n, err := sweepWebhookSubscriptions(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || strings.Join(deletedIDs, ",") != "200,202" {
		t.Errorf("deleted %d: %v, want 200,202", n, deletedIDs)
	}
}

// Package acctest provides shared helpers for acceptance and end-to-end tests.
package acctest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

var (
	runPrefixOnce sync.Once
	runPrefixVal  string
)

// RunPrefix returns a random 6-character lowercase hex string that is
// generated exactly once per process and reused on subsequent calls.
// It is safe for concurrent use.
func RunPrefix() string {
	runPrefixOnce.Do(func() {
		b := make([]byte, 3)
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("acctest: failed to generate run prefix: %v", err))
		}
		runPrefixVal = hex.EncodeToString(b)
	})
	return runPrefixVal
}

// Name returns a unique resource name for use in acceptance/E2E tests.
// The format is: tfe2e-<prefix>-<resourceType>-<n>
//
// Example:
//
//	acctest.Name("monitor", "1")  // → "tfe2e-a3f9c2-monitor-1"
func Name(resourceType, n string) string {
	return fmt.Sprintf("tfe2e-%s-%s-%s", RunPrefix(), resourceType, n)
}

// LabelText returns a label-text-safe identifier matching the API regex
// ^[a-zA-Z_][a-zA-Z0-9_]*. The format is: tfe2e_<prefix>_label_<suffix>.
// Hyphens in the suffix are replaced with underscores so callers can pass
// the same descriptive suffixes used with Name().
//
// Example:
//
//	acctest.LabelText("alarm-basic")  // → "tfe2e_a3f9c2_label_alarm_basic"
func LabelText(suffix string) string {
	s := strings.ReplaceAll(suffix, "-", "_")
	return fmt.Sprintf("tfe2e_%s_label_%s", RunPrefix(), s)
}

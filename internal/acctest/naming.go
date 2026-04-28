// Package acctest provides shared helpers for acceptance and end-to-end tests.
package acctest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

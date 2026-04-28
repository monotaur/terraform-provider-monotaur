package provider_test

import (
	"fmt"
	"math/rand"
)

// randomName returns a random name with the given prefix, suitable for use as
// a unique resource name in acceptance tests to avoid conflicts between runs.
//
// Example:
//
//	name := randomName("test-label")  // → "test-label-47823"
func randomName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, rand.Intn(100000))
}

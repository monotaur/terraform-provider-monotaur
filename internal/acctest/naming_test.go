package acctest_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monotaur/terraform-provider-monotaur/internal/acctest"
)

func TestRunPrefix_isSixHexChars(t *testing.T) {
	prefix := acctest.RunPrefix()

	if len(prefix) != 6 {
		t.Errorf("RunPrefix() length: want 6, got %d (value: %q)", len(prefix), prefix)
	}

	matched, _ := regexp.MatchString(`^[0-9a-f]{6}$`, prefix)
	if !matched {
		t.Errorf("RunPrefix() = %q: want 6 lowercase hex characters", prefix)
	}
}

func TestRunPrefix_isStableAcrossCalls(t *testing.T) {
	first := acctest.RunPrefix()
	second := acctest.RunPrefix()
	third := acctest.RunPrefix()

	if first != second {
		t.Errorf("RunPrefix() is not stable: first=%q, second=%q", first, second)
	}
	if first != third {
		t.Errorf("RunPrefix() is not stable: first=%q, third=%q", first, third)
	}
}

func TestName_returnsExpectedFormat(t *testing.T) {
	prefix := acctest.RunPrefix()
	got := acctest.Name("monitor", "1")

	want := "tfe2e-" + prefix + "-monitor-1"
	if got != want {
		t.Errorf("Name(%q, %q) = %q, want %q", "monitor", "1", got, want)
	}
}

func TestName_containsResourceTypeAndN(t *testing.T) {
	tests := []struct {
		resourceType string
		n            string
	}{
		{"monitor", "1"},
		{"label", "basic"},
		{"sensor", "42"},
	}

	for _, tt := range tests {
		got := acctest.Name(tt.resourceType, tt.n)

		if !strings.Contains(got, tt.resourceType) {
			t.Errorf("Name(%q, %q) = %q: missing resourceType", tt.resourceType, tt.n, got)
		}
		if !strings.Contains(got, tt.n) {
			t.Errorf("Name(%q, %q) = %q: missing n", tt.resourceType, tt.n, got)
		}
		if !strings.HasPrefix(got, "tfe2e-") {
			t.Errorf("Name(%q, %q) = %q: want prefix %q", tt.resourceType, tt.n, got, "tfe2e-")
		}
	}
}

func TestName_includesRunPrefix(t *testing.T) {
	prefix := acctest.RunPrefix()
	got := acctest.Name("alarm", "2")

	if !strings.Contains(got, prefix) {
		t.Errorf("Name(%q, %q) = %q: does not contain RunPrefix() %q", "alarm", "2", got, prefix)
	}
}

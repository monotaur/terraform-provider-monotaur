package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestParseTimeSpan(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"00:05:00", 5 * time.Minute, true},
		{"0:5:0", 5 * time.Minute, true},
		{"1.00:00:00", 24 * time.Hour, true},
		{"2.03:04:05", 51*time.Hour + 4*time.Minute + 5*time.Second, true},
		{"00:00:01.5", 1500 * time.Millisecond, true},
		{"00:00:00.0000001", 100 * time.Nanosecond, true},
		{"-00:01:00", -time.Minute, true},
		{"5m", 0, false},
		{"", 0, false},
		{"00:05", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseTimeSpan(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseTimeSpan(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFlattenTimeSpan(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name  string
		api   *string
		prior types.String
		want  types.String
	}{
		{"nil clears", nil, types.StringValue("00:05:00"), types.StringNull()},
		{"no prior", s("00:05:00"), types.StringNull(), types.StringValue("00:05:00")},
		{"unknown prior", s("00:05:00"), types.StringUnknown(), types.StringValue("00:05:00")},
		{"same duration keeps prior spelling", s("00:05:00"), types.StringValue("0:5:0"), types.StringValue("0:5:0")},
		{"changed duration takes API value", s("00:10:00"), types.StringValue("0:5:0"), types.StringValue("00:10:00")},
		{"unparseable prior takes API value", s("00:05:00"), types.StringValue("5m"), types.StringValue("00:05:00")},
	}
	for _, tc := range cases {
		if got := flattenTimeSpan(tc.api, tc.prior); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

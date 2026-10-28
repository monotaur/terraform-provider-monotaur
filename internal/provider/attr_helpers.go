package provider

import (
	"regexp"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// int64ToInt32Ptr returns a pointer to v as an int32, or nil when v is null or
// unknown. The API's integer attributes are int32; the framework's are int64.
func int64ToInt32Ptr(v types.Int64) *int32 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int32(v.ValueInt64())
	return &i
}

// int32PtrToInt64 maps an optional API integer onto a Terraform value.
func int32PtrToInt64(v *int32) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

// timePtrToString maps an optional API timestamp onto an RFC 3339 string.
func timePtrToString(v *time.Time) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(v.Format(time.RFC3339))
}

// timeSpanPattern matches the .NET TimeSpan constant format the API uses:
// [-][d.]hh:mm:ss[.fffffff].
var timeSpanPattern = regexp.MustCompile(`^(-)?(?:(\d+)\.)?(\d{1,2}):(\d{1,2}):(\d{1,2})(?:\.(\d{1,7}))?$`)

// parseTimeSpan parses a .NET TimeSpan string. ok is false when s is not in
// that format.
func parseTimeSpan(s string) (d time.Duration, ok bool) {
	m := timeSpanPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	days, _ := strconv.ParseInt("0"+m[2], 10, 64)
	hours, _ := strconv.ParseInt(m[3], 10, 64)
	minutes, _ := strconv.ParseInt(m[4], 10, 64)
	seconds, _ := strconv.ParseInt(m[5], 10, 64)
	var frac int64
	if m[6] != "" {
		frac, _ = strconv.ParseInt((m[6] + "0000000")[:7], 10, 64)
	}
	d = time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(frac)*100*time.Nanosecond
	if m[1] == "-" {
		d = -d
	}
	return d, true
}

// flattenTimeSpan maps an optional API TimeSpan onto a Terraform value. The API
// normalises TimeSpans (e.g. "0:5:0" comes back as "00:05:00"), so when prior
// holds the same duration in another spelling it is kept, avoiding a spurious
// diff or an "inconsistent result after apply" error.
func flattenTimeSpan(v *string, prior types.String) types.String {
	if v == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		a, okA := parseTimeSpan(*v)
		b, okB := parseTimeSpan(prior.ValueString())
		if okA && okB && a == b {
			return prior
		}
	}
	return types.StringValue(*v)
}

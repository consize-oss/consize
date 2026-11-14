package inventory

import (
	"net/url"
	"strings"
	"testing"
)

func TestQueryRejectsAmbiguousAndUnboundedInput(t *testing.T) {
	for _, raw := range []string{"tab=nope", "limit=0", "limit=201", "limit=-1", "limit=x", "owner=", "owner=a&owner=b", "unsupported=x", "telemetry=healthy", "policy=missing", "actions=auto", "environment=prod", "lifecycle_state=gone", "search=%00", "search=" + strings.Repeat("x", 513), "cursor=" + strings.Repeat("x", 2049)} {
		values, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ParseQuery(values); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	q, err := ParseQuery(url.Values{"limit": {"2"}, "tab": {"attention"}, "search": {"  Payments "}, "owner": {"unassigned"}})
	if err != nil || q.Limit != 2 || q.Search != "payments" {
		t.Fatalf("%+v %v", q, err)
	}
}

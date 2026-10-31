package evidence

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

func fixture() Record {
	now := time.Date(2026, 10, 31, 7, 0, 0, 0, time.UTC)
	return Record{ResourceID: "resource:one", PluginID: "provider-one", Signal: "memory_p95", Query: "namespace=example", Kind: Aggregated, Aggregation: "p95", Unit: "bytes", OriginalUnit: "MiB", Scale: 1048576, Value: Number(0), SampleCount: 10, WindowStart: now.Add(-time.Hour), WindowEnd: now, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: Valid, Extensions: map[string]string{"provider.series": "one"}}
}
func TestIdentityRoundTripAndIsolation(t *testing.T) {
	input := fixture()
	r, err := Seal(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Extensions["provider.series"] = "changed"
	*input.Value = 99
	if r.Extensions["provider.series"] != "one" || *r.Value != 0 {
		t.Fatal("mutable alias")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out Record
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if err = out.Validate(); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(out)
	if string(b) != string(again) {
		t.Fatal("nondeterministic serialization")
	}
	out.Query = "changed"
	if !errors.Is(out.Validate(), ErrInvalid) {
		t.Fatal("tampering accepted")
	}
	other := fixture()
	other.PluginID = "provider-two"
	r2, _ := Seal(other)
	if r.ID == r2.ID {
		t.Fatal("plugin provenance lost")
	}
	shifted := fixture()
	zone := time.FixedZone("offset", 3600)
	shifted.WindowStart = shifted.WindowStart.In(zone)
	shifted.WindowEnd = shifted.WindowEnd.In(zone)
	shifted.CollectedAt = shifted.CollectedAt.In(zone)
	shifted.FreshUntil = shifted.FreshUntil.In(zone)
	r3, _ := Seal(shifted)
	if r.ID != r3.ID {
		t.Fatal("equivalent time changed identity")
	}
}
func TestQualityAndFreshness(t *testing.T) {
	for _, quality := range []string{Valid, Missing, Stale, Partial, Invalid, Unavailable} {
		t.Run(quality, func(t *testing.T) {
			r := fixture()
			r.Quality = quality
			if quality == Missing || quality == Invalid || quality == Unavailable {
				r.Value = nil
				r.SampleCount = 0
			}
			r, err := Seal(r)
			if err != nil {
				t.Fatal(err)
			}
			if r.Usable(r.CollectedAt) != (quality == Valid) {
				t.Fatal("untrustworthy evidence usable")
			}
			if r.Usable(r.FreshUntil.Add(time.Nanosecond)) {
				t.Fatal("stale evidence usable")
			}
		})
	}
	r := fixture()
	r.Quality = Missing
	if _, err := Seal(r); err == nil {
		t.Fatal("missing numeric zero accepted")
	}
}
func TestInvalidRecords(t *testing.T) {
	for _, mutate := range []func(*Record){func(r *Record) { r.PluginID = "" }, func(r *Record) { r.WindowStart = r.WindowEnd }, func(r *Record) { r.Value = Number(math.NaN()) }, func(r *Record) { r.Scale = math.Inf(1) }, func(r *Record) { r.SampleCount = -1 }, func(r *Record) { r.Kind = Derived }, func(r *Record) { r.Extensions["large"] = string(make([]byte, 1025)) }, func(r *Record) { r.Quality = "unknown" }} {
		r := fixture()
		mutate(&r)
		if _, err := Seal(r); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
	r := fixture()
	r.Kind = Raw
	r.Aggregation = "none"
	if _, err := Seal(r); err != nil {
		t.Fatal(err)
	}
	parent, _ := Seal(fixture())
	r.Kind = Derived
	r.Parents = []string{parent.ID}
	if _, err := Seal(r); err != nil {
		t.Fatal(err)
	}
}
func TestNormalization(t *testing.T) {
	for _, tc := range []struct {
		unit, want string
		in, out    float64
	}{{"MiB", "bytes", 2, 2097152}, {"millicores", "cores", 500, 0.5}, {"bytes", "bytes", 0, 0}} {
		out, unit, _, err := Normalize(tc.in, tc.unit)
		if err != nil || out != tc.out || unit != tc.want {
			t.Fatalf("normalization %s: %v %v", tc.unit, out, err)
		}
	}
	if _, _, _, err := Normalize(1, "unknown"); err == nil {
		t.Fatal("unknown unit accepted")
	}
}

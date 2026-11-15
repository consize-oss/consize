package quality

import (
	"math"
	"testing"
)

func TestThresholdsRejectUnsafeConfiguration(t *testing.T) {
	for _, c := range []Config{{MinHistory: "bad"}, {MinHistory: "-1s"}, {MinHistory: "10m"}, {MaxAge: "0s"}, {MinimumCoverage: .4}, {MinimumCoverage: math.NaN()}, {MinimumSamples: 1}, {MaximumGapSteps: -1}, {MaximumPeakRatio: math.Inf(1)}} {
		if _, err := Resolve(c, Recommendation, false, false); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	standard, _ := Resolve(Config{}, Recommendation, true, false)
	lab, _ := Resolve(Config{}, Recommendation, false, true)
	if standard.MinHistory != "168h" || lab.MinHistory != "30m" || !standard.RequireTopology {
		t.Fatal("unsafe defaults")
	}
	if _, err := Resolve(Config{}, "unsupported", false, false); err == nil {
		t.Fatal("unknown purpose accepted")
	}
}

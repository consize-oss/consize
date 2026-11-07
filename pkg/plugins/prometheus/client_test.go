package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

func TestHTTPMetricsPreserveFractionalWindow(t *testing.T) {
	for _, fraction := range []time.Duration{123456789, 123000000, 0} {
		t.Run(fraction.String(), func(t *testing.T) {
			end := time.Now().UTC().Truncate(time.Second).Add(fraction)
			start := end.Add(-30 * time.Minute)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				from, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("start"))
				if err != nil {
					t.Error(err)
					http.Error(w, "invalid start", 400)
					return
				}
				to, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("end"))
				if err != nil {
					t.Error(err)
					http.Error(w, "invalid end", 400)
					return
				}
				if from.Before(start) || to.After(end) {
					t.Errorf("query widened interval: %s to %s", from, to)
				}
				values := [][2]any{}
				for at := from; !at.After(to); at = at.Add(30 * time.Second) {
					values = append(values, [2]any{float64(at.UnixMilli()) / 1000, "1"})
				}
				json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"metric": map[string]string{}, "values": values}}}})
			}))
			defer server.Close()
			p, err := New(Config{BaseURL: server.URL, Step: 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := p.ReadMetricsBetween(context.Background(), resource.Resource{ID: "test", Type: resource.TypeKubernetesDeployment, Name: "checkout-api", Metadata: map[string]any{"namespace": "test"}}, start, end)
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range snapshot.Records {
				if record.Quality != evidence.Valid {
					t.Errorf("%s quality=%s", record.Signal, record.Quality)
				}
			}
			for signal, coverage := range snapshot.Coverage {
				if coverage.FirstSample.Before(start) || coverage.LastSample.After(end) || coverage.ActualPoints < 60 {
					t.Errorf("%s invalid coverage: %+v", signal, coverage)
				}
			}
		})
	}
}

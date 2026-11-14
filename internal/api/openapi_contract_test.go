package api

import (
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPITracksImplementedRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../openapi/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	var got []string
	for path, value := range paths {
		operations := value.(map[string]any)
		for method := range operations {
			if method == "parameters" {
				continue
			}
			got = append(got, strings.ToUpper(method)+" "+path)
		}
	}
	sort.Strings(got)
	want := []string{
		"GET /api/action-records", "GET /api/actions", "GET /api/audit", "GET /api/audit/export",
		"GET /api/audit/health", "GET /api/audit/integrity", "GET /api/dashboard", "GET /api/health", "GET /api/jobs",
		"GET /api/plugins", "GET /api/resources", "POST /api/discovery",
		"POST /api/recommendations/generate", "POST /api/recommendations/{recommendation_id}/execute",
		"POST /api/recommendations/{recommendation_id}/plan", "POST /api/recommendations/{recommendation_id}/recover",
		"POST /api/resources", "POST /api/recommendations/{recommendation_id}/cancel",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("OpenAPI routes do not match implemented API routes\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOpenAPIDeclaresRequestCorrelation(t *testing.T) {
	raw, err := os.ReadFile("../../openapi/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "X-Request-ID") || !strings.Contains(string(raw), "RequestId:") {
		t.Fatal("OpenAPI must declare the response correlation header")
	}
}

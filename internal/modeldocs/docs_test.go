// Package modeldocs validates the published examples against implemented contracts.
package modeldocs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

const docsRoot = "../../docs"

func readExample(t *testing.T, name string, out any) {
	t.Helper()
	file, err := os.Open(filepath.Join(docsRoot, "reference/examples/core-model", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("%s: multiple or invalid JSON documents", name)
	}
}

func TestCoreContractExamples(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	var input, normalized resource.Resource
	readExample(t, "kubernetes-resource-input", &input)
	readExample(t, "kubernetes-resource", &normalized)
	if err := normalized.Validate(); err != nil {
		t.Fatal(err)
	}
	want, err := input.Normalize(asOf)
	if err != nil {
		t.Fatal(err)
	}
	actualJSON, _ := json.Marshal(normalized)
	expectedJSON, _ := json.Marshal(want)
	if !bytes.Equal(actualJSON, expectedJSON) || normalized.SupportStatus != resource.SupportFull {
		t.Fatal("normalized example disagrees with the input contract")
	}
	st := store.NewMemory()
	defer st.Close()
	actual, err := st.CreateResource(ctx, input)
	if err != nil || actual.ID != normalized.ID {
		t.Fatalf("resource registration: %v", err)
	}

	var measurement evidence.Record
	readExample(t, "evidence", &measurement)
	if err = measurement.Validate(); err != nil {
		t.Fatal(err)
	}
	if measurement.ResourceID != actual.ID || !measurement.Usable(asOf) || measurement.Usable(asOf.Add(time.Hour)) {
		t.Fatal("example freshness or ownership is incorrect")
	}

	var proposal store.Recommendation
	readExample(t, "recommendation-input", &proposal)
	if len(proposal.EvidenceSnapshot) != 1 || proposal.EvidenceSnapshot[0].ID != measurement.ID {
		t.Fatal("example chain lost its evidence")
	}
	rec, err := st.CreateRecommendation(ctx, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != store.RecommendationPending || rec.ResourceID != actual.ID {
		t.Fatal("example proposal is not pending for the registered resource")
	}

	var request store.Action
	readExample(t, "action-input", &request)
	if err = request.Validate(); err != nil {
		t.Fatal(err)
	}
	action, err := st.CreateAction(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.CreateAction(ctx, request)
	if err != nil || again.ID != action.ID || action.Status != store.ActionRequested || action.RecommendationID != rec.ID {
		t.Fatal("example action/idempotency contract mismatch")
	}

	var future resource.Resource
	readExample(t, "future-rds-resource-input", &future)
	future, err = future.Normalize(asOf)
	if err != nil || future.SupportStatus != resource.SupportModelOnly {
		t.Fatalf("future-provider example must be model-only: %v", err)
	}
}

func TestCoreDocumentationLocalLinks(t *testing.T) {
	// Scope this check to the contract entry point and the normative pages it owns;
	// external websites and unrelated release/marketing docs are not a PR dependency.
	pages := []string{
		"reference/core-model.md", "reference/resource-model.md", "reference/resource-registry.md",
		"reference/evidence.md", "engineering/recommendation-action-contracts.md",
		"engineering/adr-0001-universal-resource-identity.md", "engineering/adr-0002-proposals-attempts-and-evidence.md", "engineering/runtime-data.md",
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, page := range pages {
		path := filepath.Join(docsRoot, page)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range links.FindAllStringSubmatch(string(body), -1) {
			href := strings.Trim(match[1], "<>")
			const sourcePrefix = "https://github.com/consize-oss/consize/blob/main/"
			if strings.HasPrefix(href, sourcePrefix) {
				target := strings.SplitN(strings.TrimPrefix(href, sourcePrefix), "#", 2)[0]
				if _, err = os.Stat(filepath.Join("../..", target)); err != nil {
					t.Errorf("%s links to missing source %s: %v", page, href, err)
				}
				continue
			}
			if strings.Contains(href, "://") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "#") {
				continue
			}
			target := strings.SplitN(href, "#", 2)[0]
			if _, err = os.Stat(filepath.Join(filepath.Dir(path), target)); err != nil {
				t.Errorf("%s links to missing %s: %v", page, href, err)
			}
		}
	}
}

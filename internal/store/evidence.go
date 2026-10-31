package store

import (
	"fmt"
	"strings"
)

// Legacy references remain readable but are never promoted to verified provenance.
func validateRecommendationEvidence(rec Recommendation) error {
	if len(rec.EvidenceSnapshot) == 0 {
		for _, id := range rec.EvidenceRefs {
			if strings.HasPrefix(id, "evidence:v1:") {
				return fmt.Errorf("typed evidence reference has no snapshot")
			}
		}
		return nil
	}
	if rec.AlgorithmVersion == "" || len(rec.EvidenceSnapshot) > 256 || len(rec.EvidenceSnapshot) != len(rec.EvidenceRefs) {
		return fmt.Errorf("evidence snapshot requires explicit algorithm version and matching references")
	}
	seen := map[string]bool{}
	for i, r := range rec.EvidenceSnapshot {
		if err := r.Validate(); err != nil {
			return err
		}
		if r.ResourceID != rec.ResourceID || r.ID != rec.EvidenceRefs[i] || seen[r.ID] {
			return fmt.Errorf("recommendation evidence identity mismatch or duplicate")
		}
		seen[r.ID] = true
	}
	return nil
}

package policy

import "github.com/consize-oss/consize/pkg/resource"

// Coverage describes matching rules, never execution permission. The current
// foundation engine has one global policy; future scoped policy resolution can
// return uncovered or unknown without changing the inventory contract.
type Coverage struct {
	Status    string   `json:"status"`
	PolicyIDs []string `json:"policy_ids"`
	Reason    string   `json:"reason"`
}

func (e *Engine) Coverage(res resource.Resource) Coverage {
	if e == nil || res.ID == "" || res.Type == "" {
		return Coverage{Status: "unknown", PolicyIDs: []string{}, Reason: "policy_assessment_unavailable"}
	}
	return Coverage{Status: "covered", PolicyIDs: []string{"oss-foundation-v1"}, Reason: "global_foundation_policy"}
}

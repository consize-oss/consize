package inventory

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/consize-oss/consize/pkg/resource"
)

type Query struct {
	Provider       string `json:"provider"`
	Account        string `json:"account"`
	Region         string `json:"region"`
	Type           string `json:"type"`
	SourcePluginID string `json:"source_plugin_id"`
	Environment    string `json:"environment"`
	LifecycleState string `json:"lifecycle_state"`
	Owner          string `json:"owner"`
	Search         string `json:"search"`
	Telemetry      string `json:"telemetry"`
	Policy         string `json:"policy"`
	Actions        string `json:"actions"`
	Tab            string `json:"tab"`
	Limit          int    `json:"-"`
	Cursor         string `json:"-"`
}

func ParseQuery(values url.Values) (Query, error) {
	q := Query{Limit: 100, Tab: "all"}
	bad := func() (Query, error) {
		return Query{}, fmt.Errorf("%w: unsupported, repeated or invalid inventory filter", resource.ErrInvalidQuery)
	}
	for key, items := range values {
		if len(items) != 1 || items[0] == "" {
			return bad()
		}
		v := items[0]
		if key == "cursor" {
			if len(v) > 2048 {
				return bad()
			}
			q.Cursor = v
			continue
		}
		if len(v) > 512 || strings.ContainsAny(v, "\r\n\x00") {
			return bad()
		}
		switch key {
		case "provider":
			q.Provider = v
		case "account":
			q.Account = v
		case "region":
			q.Region = v
		case "type":
			q.Type = v
		case "source_plugin_id":
			q.SourcePluginID = v
		case "environment":
			if !oneOf(v, resource.EnvProduction, resource.EnvStaging, resource.EnvDevelopment, resource.EnvUnknown) {
				return bad()
			}
			q.Environment = v
		case "lifecycle_state":
			if !oneOf(v, resource.LifecycleActive, resource.LifecycleStale, resource.LifecycleDeleted) {
				return bad()
			}
			q.LifecycleState = v
		case "owner":
			q.Owner = v
		case "search":
			q.Search = strings.ToLower(strings.TrimSpace(v))
		case "telemetry":
			if !oneOf(v, "usable", "stale", "unmonitored", "unknown") {
				return bad()
			}
			q.Telemetry = v
		case "policy":
			if !oneOf(v, "covered", "uncovered", "unknown") {
				return bad()
			}
			q.Policy = v
		case "actions":
			if !oneOf(v, "supported", "unsupported", "unknown") {
				return bad()
			}
			q.Actions = v
		case "tab":
			if !oneOf(v, "all", "attention", "unmonitored") {
				return bad()
			}
			q.Tab = v
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 200 {
				return bad()
			}
			q.Limit = n
		default:
			return bad()
		}
	}
	return q, nil
}

func oneOf(v string, options ...string) bool {
	for _, candidate := range options {
		if v == candidate {
			return true
		}
	}
	return false
}

type Summary struct {
	Discovered             int                     `json:"discovered"`
	Reporting              int                     `json:"reporting"`
	Attention              int                     `json:"attention"`
	Unmonitored            int                     `json:"unmonitored"`
	SupportedActions       int                     `json:"supported_actions"`
	SupportedActionPercent *float64                `json:"supported_action_percent"`
	Reasons                map[AttentionReason]int `json:"reasons"`
}

// Filter computes all summary/tab counts before selecting a tab and before
// pagination. Every other filter affects counts. Deleted resources are excluded
// unless an explicit lifecycle_state filter selects them.
func Filter(rows []Row, q Query) ([]Row, Summary) {
	out := []Row{}
	s := Summary{Reasons: map[AttentionReason]int{}}
	for _, row := range rows {
		r, a := row.Resource, row.Assessment
		if q.LifecycleState == "" && r.LifecycleState == resource.LifecycleDeleted {
			continue
		}
		if q.Provider != "" && r.Provider != q.Provider || q.Account != "" && r.Account != q.Account || q.Region != "" && r.Region != q.Region || q.Type != "" && r.Type != q.Type || q.SourcePluginID != "" && r.SourcePluginID != q.SourcePluginID || q.Environment != "" && r.Environment != q.Environment || q.LifecycleState != "" && r.LifecycleState != q.LifecycleState {
			continue
		}
		if q.Owner == "unassigned" && a.Owner != nil || q.Owner != "" && q.Owner != "unassigned" && (a.Owner == nil || *a.Owner != q.Owner) {
			continue
		}
		if q.Telemetry != "" && a.Telemetry.Status != q.Telemetry || q.Policy != "" && a.Policy.Status != q.Policy || q.Actions != "" && a.Actions.Status != q.Actions {
			continue
		}
		if q.Search != "" && !matchesSearch(r, a, q.Search) {
			continue
		}
		s.Discovered++
		attention := len(a.AttentionReasons) > 0
		if attention {
			s.Attention++
		}
		if a.Telemetry.Status == "usable" {
			s.Reporting++
		}
		if a.Telemetry.Status == "unmonitored" {
			s.Unmonitored++
		}
		if a.Actions.Status == "supported" {
			s.SupportedActions++
		}
		for _, reason := range a.AttentionReasons {
			s.Reasons[reason]++
		}
		if q.Tab == "attention" && !attention || q.Tab == "unmonitored" && a.Telemetry.Status != "unmonitored" {
			continue
		}
		out = append(out, row)
	}
	if s.Discovered > 0 && s.Reasons[ActionUnknown] == 0 {
		pct := float64(s.SupportedActions) * 100 / float64(s.Discovered)
		s.SupportedActionPercent = &pct
	}
	return out, s
}

func matchesSearch(r resource.Resource, a Assessment, q string) bool {
	values := []string{r.ID, r.Name, r.Account}
	if a.Owner != nil {
		values = append(values, *a.Owner)
	}
	for k, v := range r.Labels {
		values = append(values, k, v)
	}
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

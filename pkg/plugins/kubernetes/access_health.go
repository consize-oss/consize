package kubernetes

import (
	"context"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AccessCheck struct {
	CheckedAt           time.Time `json:"checked_at"`
	Status              string    `json:"status"`
	APIReachable        *bool     `json:"api_reachable,omitempty"`
	Verb                string    `json:"verb"`
	APIGroup            string    `json:"api_group"`
	Resource            string    `json:"resource"`
	Namespace           string    `json:"namespace"`
	Code                string    `json:"code,omitempty"`
	Retryable           bool      `json:"retryable"`
	Guidance            string    `json:"guidance,omitempty"`
	NeedsSuccessfulScan bool      `json:"needs_successful_scan"`
}

func accessCheck(err error, namespace string, now time.Time) AccessCheck {
	if namespace == "" {
		namespace = "*"
	}
	out := AccessCheck{CheckedAt: now, Status: "authorized", Verb: "list", APIGroup: "apps", Resource: "deployments", Namespace: namespace}
	reachable := true
	out.APIReachable = &reachable
	if err != nil {
		e := safeScanError(err)
		out.Status = "failed"
		out.Code = e.Code
		out.Retryable = e.Retryable
		out.Guidance = accessGuidance(e.Code)
		out.NeedsSuccessfulScan = true
		if e.Code == "configuration" || e.Code == "canceled" || e.Code == "provider_failure" || e.Code == "unsupported_client" {
			out.APIReachable = nil
		}
		if e.Code == "network" || e.Code == "tls" || e.Code == "timeout" || e.Code == "unavailable" {
			reachable = false
		}
		if e.Code == "unauthorized" || e.Code == "forbidden" {
			out.Status = e.Code
		}
		if e.Namespace != "" {
			out.Namespace = e.Namespace
		}
	}
	return out
}
func (p *Plugin) recordAccess(check AccessCheck, completedScan bool) {
	p.scans.mu.Lock()
	defer p.scans.mu.Unlock()
	if !completedScan && p.scans.diagnostics.Access != nil && p.scans.diagnostics.Access.NeedsSuccessfulScan {
		check.NeedsSuccessfulScan = true
	}
	p.scans.diagnostics.Access = &check
}
func (p *Plugin) discoveryHealth(ctx context.Context) plugin.Health {
	now := time.Now().UTC()
	timeout := p.discovery.timeout
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lister, ok := p.patcher.(deploymentPageLister)
	if !ok {
		check := accessCheck(scanFailure("unsupported_client"), "", now)
		p.recordAccess(check, false)
		return plugin.Health{Status: "degraded", Message: "Scoped discovery list access cannot be verified by this client.", CheckedAt: now, Details: map[string]any{"discovery": p.DiscoveryDiagnostics()}}
	}
	namespaces := p.discovery.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	var err error
	namespace := ""
	for _, ns := range namespaces {
		namespace = ns
		_, err = lister.ListDeploymentPage(ctx, ns, metav1.ListOptions{Limit: 1, LabelSelector: p.discovery.selector.String()})
		if err != nil {
			break
		}
	}
	check := accessCheck(err, namespace, time.Now().UTC())
	p.recordAccess(check, false)
	diagnostics := p.DiscoveryDiagnostics()
	status, message := "degraded", "Deployment list access verified; a successful complete discovery scan is still required."
	if err != nil {
		message = safeScanError(err).Error()
		if check.APIReachable != nil && !*check.APIReachable {
			status = "unhealthy"
		}
	} else if !diagnostics.InProgress && diagnostics.LastAttempt != nil && diagnostics.LastAttempt.Complete && diagnostics.Access != nil && !diagnostics.Access.NeedsSuccessfulScan {
		status = "healthy"
		message = "Configured Deployment list access verified; last discovery scan complete."
	}
	ready := err == nil
	return plugin.Health{Ready: &ready, Status: status, Message: message, CheckedAt: time.Now().UTC(), Details: map[string]any{"discovery": diagnostics}}
}

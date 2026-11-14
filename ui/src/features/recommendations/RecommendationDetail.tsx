"use client";

import { useState } from "react";
import Link from "next/link";
import { WorkflowActions } from "./WorkflowActions";
import { AlertTriangle } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { formatMoney, titleCase } from "@/lib/format";
import { useRecommendationDetail } from "../overview/queries";
import type { EvidenceItem, RecommendationDetail as Detail } from "@/schemas";

// One component, two possible shells: the /recommendations/[id] page
// renders it directly today; a drawer can wrap the same component later.
// When mounting it in a drawer, pass key={id} so tab state resets per item.

type Tab = "overview" | "evidence" | "policy" | "audit";
const TABS: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "evidence", label: "Evidence" },
  { key: "policy", label: "Policy & verification" },
  { key: "audit", label: "Audit" },
];

type Tone = "low" | "medium" | "high" | "neutral";
const TONE: Record<Tone, string> = {
  low: "bg-badge-low/10 text-badge-low-text border-badge-low/25",
  medium: "bg-badge-medium/10 text-badge-medium-text border-badge-medium/25",
  high: "bg-badge-high/10 text-badge-high-text border-badge-high/25",
  neutral: "bg-app-surface-alt text-fg-secondary border-app-border",
};

// Status pills always carry text; the tone only reinforces it (PRD §18.4).
function Pill({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded border font-mono text-[11px] font-medium ${TONE[tone]}`}
    >
      {children}
    </span>
  );
}

function statusTone(status: Detail["status"]): Tone {
  switch (status) {
    case "actionable":
    case "executed":
      return "low";
    case "pending_approval":
    case "planned":
      return "medium";
    case "blocked":
      return "high";
    default:
      return "neutral"; // superseded
  }
}

function qualityTone(q: EvidenceItem["quality"]): Tone {
  return q === "valid" ? "low" : q === "partial" ? "medium" : "high";
}
function freshnessTone(f: EvidenceItem["freshness"]): Tone {
  return f === "fresh" ? "low" : f === "stale" ? "medium" : "high";
}

export function RecommendationDetail({ id }: { id: string }) {
  const [tab, setTab] = useState<Tab>("overview");
  const { data: rec, isPending, isError, error, refetch } = useRecommendationDetail(id);

  if (isPending) {
    return (
      <div aria-label="Loading recommendation" className="space-y-4 animate-pulse">
        <div className="h-8 w-2/3 rounded bg-app-surface-alt" />
        <div className="h-4 w-1/3 rounded bg-app-surface-alt" />
        <div className="h-64 rounded-lg bg-app-surface border border-app-border" />
      </div>
    );
  }

  if (isError) {
    const gone = error instanceof ApiError && error.status === 404;
    return (
      <div role="alert" className="rounded-lg border border-app-border bg-app-surface p-6">
        <p className="text-sm font-medium text-fg">
          {gone ? "This recommendation no longer exists" : "Couldn't load this recommendation"}
        </p>
        <p className="text-xs text-fg-muted mt-1">
          {gone
            ? "It may have been removed or replaced since you last opened it."
            : error instanceof Error
              ? error.message
              : "The recommendation endpoint is unavailable."}
        </p>
        <div className="mt-4 flex gap-2">
          <Link
            href="/recommendations"
            className="h-8 px-3 inline-flex items-center rounded-lg border border-app-border text-xs text-fg hover:bg-app-surface-alt"
          >
            Back to recommendations
          </Link>
          {!gone && (
            <button
              onClick={() => void refetch()}
              className="h-8 px-3 rounded-lg border border-app-border text-xs text-fg hover:bg-app-surface-alt"
            >
              Retry
            </button>
          )}
        </div>
      </div>
    );
  }

  const savings = formatMoney(rec.estimated_savings);
  const superseded = rec.status === "superseded";

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-3xl font-semibold tracking-tight text-fg">{rec.title}</h1>
          <Pill tone={statusTone(rec.status)}>{titleCase(rec.workflow_state ?? rec.status)}</Pill>
        </div>
        <p className="text-sm text-fg-muted">
          {rec.resource.name} · {rec.resource.provider} · {rec.resource.environment}
          {rec.resource.namespace ? ` · ${rec.resource.namespace}` : ""}
        </p>
        <p className="font-mono text-xs text-fg-muted">
          {rec.id} · version {rec.version} · owner {rec.owner}
        </p>
      </header>

      {superseded && (
        <div
          role="status"
          className={`flex items-start gap-3 rounded-lg border p-4 text-sm ${TONE.medium}`}
        >
          <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
          <p>
            This recommendation was superseded
            {rec.superseded_by ? (
              <>
                {" "}
                by{" "}
                <Link className="underline" href={`/recommendations/${rec.superseded_by}`}>
                  {rec.superseded_by}
                </Link>
              </>
            ) : null}
            . It stays inspectable for history, but it can&apos;t be executed.
          </p>
        </div>
      )}

      <div role="tablist" aria-label="Recommendation sections" className="flex gap-1 border-b border-app-border">
        {TABS.map((t) => (
          <button
            key={t.key}
            role="tab"
            id={`tab-${t.key}`}
            tabIndex={tab === t.key ? 0 : -1}
            onKeyDown={event => {
              if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
              event.preventDefault();
              const index = TABS.findIndex(item => item.key === tab);
              const next = event.key === "Home" ? 0 : event.key === "End" ? TABS.length - 1 : (index + (event.key === "ArrowRight" ? 1 : -1) + TABS.length) % TABS.length;
              setTab(TABS[next].key);
              document.getElementById(`tab-${TABS[next].key}`)?.focus();
            }}
            aria-selected={tab === t.key}
            aria-controls={`panel-${t.key}`}
            onClick={() => setTab(t.key)}
            className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === t.key
                ? "border-emerald-500 text-fg"
                : "border-transparent text-fg-muted hover:text-fg"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "overview" && <OverviewTab rec={rec} savings={savings} />}
        {tab === "evidence" && <EvidenceTab evidence={rec.evidence} />}
        {tab === "policy" && <PolicyTab rec={rec} />}
        {tab === "audit" && <AuditTab rec={rec} />}
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="bg-app-surface border border-app-border rounded-lg p-5">
      <h2 className="text-base font-semibold text-fg mb-3">{title}</h2>
      {children}
    </section>
  );
}

function OverviewTab({
  rec,
  savings,
}: {
  rec: Detail;
  savings: { display: string; basisLabel: string };
}) {
  const keys = Array.from(new Set([...Object.keys(rec.current), ...Object.keys(rec.proposed)]));
  const coverage = Math.round(rec.confidence_assessment.coverage_ratio * 100);

  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <div className="lg:col-span-2 space-y-6">
        <Section title="Why this recommendation">
          <p className="text-sm text-fg-secondary">{rec.summary}</p>
          <p className="font-mono text-xs text-fg-muted mt-3">
            Algorithm {rec.algorithm.id} v{rec.algorithm.version}
          </p>
        </Section>

        <Section title="Proposed change">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left font-mono text-[11px] uppercase tracking-wider text-fg-muted">
                <th className="py-2 font-semibold">Setting</th>
                <th className="py-2 font-semibold">Current</th>
                <th className="py-2 font-semibold">Proposed</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-app-border">
              {keys.map((k) => (
                <tr key={k}>
                  <th scope="row" className="py-2 pr-4 text-left font-medium text-fg">
                    {k}
                  </th>
                  <td className="py-2 pr-4 text-fg-secondary">{rec.current[k] ?? "—"}</td>
                  <td className="py-2 text-fg">{rec.proposed[k] ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Section>

        <Section title="Confidence">
          <div className="flex items-center gap-2 mb-2">
            <Pill tone={rec.confidence === "high" ? "low" : rec.confidence === "medium" ? "medium" : "high"}>
              {titleCase(rec.confidence)}
            </Pill>
            <span className="text-xs text-fg-muted">
              {rec.confidence_assessment.observed_history} observed of{" "}
              {rec.confidence_assessment.required_history} required · {coverage}% coverage
            </span>
          </div>
          <ul className="list-disc pl-5 text-sm text-fg-secondary space-y-1">
            {rec.confidence_assessment.reasons.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
          
          <p className="text-xs text-fg-muted mt-3">
            Confidence reflects evidence quality and heuristic certainty. It is not a guarantee that
            production demand has been fully observed.
          </p>
        </Section>

        <Section title="Workload classification">
          {rec.workload_classification ? (
            <p className="text-sm text-fg-secondary">
              {titleCase(rec.workload_classification.class)}{" "}
              <span className="font-mono text-xs text-fg-muted">
                (classifier v{rec.workload_classification.classifier_version})
              </span>
            </p>
          ) : (
            
            <p className="text-sm text-fg-secondary">
              Not available yet. Consize doesn&apos;t classify this workload, so steady versus bursty
              behavior is unverified. Treat headroom assumptions accordingly.
            </p>
          )}
        </Section>
      </div>

      <div className="space-y-6">
        <Section title="Estimated savings">
          <p className="font-mono text-2xl font-bold text-fg">
            {savings.display}
            <span className="text-xs font-normal text-fg-muted"> /mo</span>
          </p>
          <p className="text-xs text-fg-muted mt-1">Basis: {savings.basisLabel}</p>
          {rec.estimated_savings.basis === "estimated" && (
            <p className="text-xs text-fg-muted mt-2">
              Pricing-list estimate. Not verified or realized savings.
            </p>
          )}
        </Section>

        <WorkflowActions id={rec.id} eligible={rec.status === "pending_approval" || rec.status === "actionable"} />

      </div>
    </div>
  );
}

function EvidenceTab({ evidence }: { evidence: EvidenceItem[] }) {
  if (evidence.length === 0) {
    return (
      <Section title="Evidence">
        <p className="text-sm text-fg-secondary">
          No evidence is attached to this recommendation. Without evidence it can&apos;t be trusted
          for execution.
        </p>
      </Section>
    );
  }
  return (
    <Section title="Evidence">
      {/* Horizontal scroll */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left font-mono text-[11px] uppercase tracking-wider text-fg-muted">
              <th className="py-2 pr-4 font-semibold">Signal</th>
              <th className="py-2 pr-4 font-semibold">Value</th>
              <th className="py-2 pr-4 font-semibold">Window</th>
              <th className="py-2 pr-4 font-semibold">Source</th>
              <th className="py-2 pr-4 font-semibold">Freshness</th>
              <th className="py-2 pr-4 font-semibold">Quality</th>
              <th className="py-2 font-semibold">Reason codes</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-app-border">
            {evidence.map((e) => (
              <tr key={`${e.signal}-${e.aggregation}`}>
                <th scope="row" className="py-2 pr-4 text-left font-medium text-fg">
                  {e.signal}
                  {e.aggregation && (
                    <span className="block font-mono text-[11px] font-normal text-fg-muted">
                      {e.aggregation}
                    </span>
                  )}
                </th>
                <td className="py-2 pr-4 font-mono text-fg">
                  {/* null is "no data", never zero. */}
                  {e.value === null ? (
                    <span className="text-fg-muted">No data</span>
                  ) : (
                    `${e.value}${e.unit ? ` ${e.unit}` : ""}`
                  )}
                </td>
                <td className="py-2 pr-4 text-fg-secondary">{e.window}</td>
                <td className="py-2 pr-4 text-fg-secondary">{e.source}</td>
                <td className="py-2 pr-4">
                  <Pill tone={freshnessTone(e.freshness)}>{titleCase(e.freshness)}</Pill>
                </td>
                <td className="py-2 pr-4">
                  <Pill tone={qualityTone(e.quality)}>{titleCase(e.quality)}</Pill>
                </td>
                <td className="py-2 font-mono text-[11px] text-fg-muted">
                  {e.reason_codes.length ? e.reason_codes.join(", ") : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

function PolicyTab({ rec }: { rec: Detail }) {
  const tone: Tone =
    rec.policy.decision === "allowed" ? "low" : rec.policy.decision === "blocked" ? "high" : "medium";
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Section title="Policy decision">
        <div className="flex items-center gap-2 mb-3">
          <Pill tone={tone}>{titleCase(rec.policy.decision)}</Pill>
          <span className="font-mono text-xs text-fg-muted">{rec.policy_id}</span>
        </div>
        <ul className="list-disc pl-5 text-sm text-fg-secondary space-y-1">
          {rec.policy.reasons.map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
        {rec.policy.required_approval && (
          <p className="text-sm text-fg-secondary mt-3">
            Required approval: <span className="text-fg font-medium">{rec.policy.required_approval}</span>
          </p>
        )}
      </Section>
      <Section title="Verification plan">
        <ul className="list-disc pl-5 text-sm text-fg-secondary space-y-1">
          {rec.policy.verification.map((v) => (
            <li key={v}>{v}</li>
          ))}
        </ul>
      </Section>
    </div>
  );
}

function AuditTab({ rec }: { rec: Detail }) {
  return (
    <Section title="Audit history">
      {rec.audit.length === 0 ? (
        <p className="text-sm text-fg-secondary">No audit events for this recommendation yet.</p>
      ) : (
        <ol className="divide-y divide-app-border">
          {rec.audit.map((e) => (
            <li key={e.id} className="py-3 flex items-baseline justify-between gap-4">
              <div className="min-w-0">
                <p className="text-sm font-medium text-fg">{e.title}</p>
                <p className="text-xs text-fg-muted">
                  Actor: {e.actor}
                  <span className="font-mono"> · {e.correlation_id}</span>
                </p>
              </div>
              <time dateTime={e.occurred_at} className="font-mono text-xs text-fg-muted shrink-0">
                {new Date(e.occurred_at).toLocaleString()}
              </time>
            </li>
          ))}
        </ol>
      )}
    </Section>
  );
}
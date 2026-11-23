"use client";

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { ArrowUpRight, X } from "lucide-react";
import { relativeTime, titleCase } from "@/lib/format";
import { resourceConcerns, type ResourceConcern, type ResourceInventoryItem } from "./resourceInventoryModel";

const CONCERN_LABELS: Record<ResourceConcern, string> = {
  unowned: "Owner missing", stale: "Stale telemetry", unmonitored: "Unmonitored",
  policy: "Policy gap",
};

const RESOURCE_TYPES: Record<string, string> = {
  "kubernetes.deployment": "Kubernetes deployment",
  "kubernetes.node_group": "Kubernetes node group",
  "aws.rds.instance": "Amazon RDS instance",
  "aws.elasticache.cluster": "ElastiCache cluster",
  "storage.bucket": "Storage bucket",
  "llm.application": "LLM application",
};

type EvidenceFact = { label: string; value: string; dateTime?: string };
type ConcernContent = { title: string; description: string; evidence: EvidenceFact[]; nextSteps: string[] };

function concernContent(resource: ResourceInventoryItem, concern: ResourceConcern, asOf: number): ConcernContent {
  const { telemetry, assessed_at: assessedAt } = resource.inventory;
  const type = RESOURCE_TYPES[resource.type] ?? resource.inventory.scope_type ?? resource.type;
  const signals = telemetry?.signals?.length ? new Intl.ListFormat("en").format(telemetry.signals) : "Not available";
  const lastChecked: EvidenceFact = {
    label: "Last checked", value: assessedAt ? relativeTime(assessedAt, asOf) : "Not available", dateTime: assessedAt,
  };
  const lastReceived: EvidenceFact = telemetry?.checked_at
    ? { label: "Last received", value: relativeTime(telemetry.checked_at, asOf), dateTime: telemetry.checked_at }
    : { label: "Last received", value: "No reports available" };

  switch (concern) {
    case "unowned":
      return {
        title: "A responsible team needs to be identified",
        description: "This resource has no assigned owner to contact about its coverage concerns.",
        evidence: [
          { label: "Owner", value: "No owner assigned" },
          { label: "Resource type", value: type },
          { label: "Account", value: resource.account || "—" },
          lastChecked,
        ],
        nextSteps: ["Identify the team responsible for this resource and update its ownership record."],
      };
    case "stale":
      return {
        title: "Fresh measurements are needed",
        description: "The monitoring information is older than allowed.",
        evidence: [
          { label: "Source", value: telemetry?.source || "Not available" },
          { label: "Signals", value: signals },
          lastChecked,
          lastReceived,
          { label: "Maximum report age", value: telemetry?.freshness_limit || "Not available" },
        ],
        nextSteps: [
          "Check that the monitoring source can reach this resource.",
          telemetry?.signals?.length ? `Confirm that ${signals} measurements are arriving.` : "Confirm that monitoring measurements are arriving.",
          "Recheck after a fresh report arrives.",
        ],
      };
    case "unmonitored":
      return {
        title: "Monitoring data is missing",
        description: "The assessment confirms there are no monitoring reports available for this resource.",
        evidence: [
          { label: "Monitoring source", value: telemetry?.source || "Not available" },
          { label: "Signals", value: "Not available" },
          lastChecked,
          { label: "Last received", value: "No reports available" },
          { label: "Owner", value: resource.owner.trim() || "No owner assigned" },
        ],
        nextSteps: [
          "Confirm that a monitoring source is configured to collect measurements for this resource.",
          "Recheck after its first monitoring report arrives.",
        ],
      };
    case "policy":
      return {
        title: "This resource has no policy coverage",
        description: "The coverage assessment found no matching policy for this resource.",
        evidence: [
          { label: "Policy", value: "No matching policy" },
          { label: "Resource type", value: type },
          { label: "Environment", value: titleCase(resource.environment) },
          lastChecked,
        ],
        nextSteps: [
          "Confirm which policy should apply to this resource.",
          "Review that policy’s scope, then recheck coverage after it is corrected.",
        ],
      };
  }
}

export function ResourceGapDetailsPanel({ resource, asOf, onRecheck, onClose }: {
  resource: ResourceInventoryItem;
  asOf: number;
  onRecheck: () => Promise<boolean>;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const tabRefs = useRef<Partial<Record<ResourceConcern, HTMLButtonElement | null>>>({});
  const id = useId();
  const [selectedConcern, setSelectedConcern] = useState<ResourceConcern | null>(null);
  const [recheckStatus, setRecheckStatus] = useState<"idle" | "checking" | "done" | "error">("idle");
  const concerns = resourceConcerns(resource);
  // A fresh assessment can resolve the selected concern while the panel is open.
  const activeConcern = selectedConcern && concerns.includes(selectedConcern) ? selectedConcern : concerns[0];
  const content = activeConcern ? concernContent(resource, activeConcern, asOf) : null;
  const provider = resource.provider === "aws" ? "AWS" : titleCase(resource.provider);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) {
      dialog.showModal();
      headingRef.current?.focus();
    }
  }, []);

  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 });
  }, [activeConcern]);

  function navigateConcerns(event: KeyboardEvent<HTMLButtonElement>, concern: ResourceConcern) {
    let index = concerns.indexOf(concern);
    if (event.key === "ArrowRight") index = (index + 1) % concerns.length;
    else if (event.key === "ArrowLeft") index = (index + concerns.length - 1) % concerns.length;
    else if (event.key === "Home") index = 0;
    else if (event.key === "End") index = concerns.length - 1;
    else return;
    event.preventDefault();
    const next = concerns[index];
    setSelectedConcern(next);
    tabRefs.current[next]?.focus();
  }

  async function recheck() {
    setRecheckStatus("checking");
    try {
      setRecheckStatus(await onRecheck() ? "done" : "error");
    } catch {
      setRecheckStatus("error");
    }
  }

  return (
    <dialog ref={dialogRef} aria-labelledby={`${id}-title`} onClose={onClose}
      className="fixed inset-y-0 right-0 left-auto m-0 h-dvh max-h-none w-full max-w-[520px] overflow-hidden border-l border-app-border bg-app-surface p-0 text-fg shadow-2xl backdrop:bg-black/40"
    >
      <div className="flex h-full min-h-0 flex-col">
        <header className="shrink-0 border-b border-app-border px-5 pb-5 pt-4 sm:px-6">
          <div className="mb-3 flex items-center justify-between gap-3">
            <p className="text-xs text-fg-muted">Coverage gap</p>
            <button type="button" aria-label="Close gap details" onClick={() => dialogRef.current?.close()}
              className="grid size-9 cursor-pointer place-items-center rounded-btn text-fg-secondary hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-accent-500"
            ><X aria-hidden="true" className="size-4" /></button>
          </div>
          <h2 ref={headingRef} tabIndex={-1} id={`${id}-title`} className="break-words text-lg font-semibold focus:outline-none">{resource.name}</h2>
          <p className="mt-1 break-words text-xs leading-5 text-fg-muted">
            {provider} · {titleCase(resource.environment)} · {resource.owner.trim() || "Owner unassigned"}
          </p>
          {concerns.length > 0 && (
            <div role="tablist" aria-label="Resource concerns" className="mt-4 flex flex-wrap gap-2">
              {concerns.map((concern) => (
                <button key={concern} ref={(node) => { tabRefs.current[concern] = node; }}
                  type="button" role="tab" id={`${id}-${concern}`} aria-controls={`${id}-content`}
                  aria-selected={activeConcern === concern} tabIndex={activeConcern === concern ? 0 : -1}
                  onClick={() => setSelectedConcern(concern)} onKeyDown={(event) => navigateConcerns(event, concern)}
                  className={"inline-flex min-h-9 cursor-pointer items-center gap-1.5 rounded-btn border px-3 py-1.5 text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 " + (
                    activeConcern === concern ? "border-ok-line bg-ok-bg font-medium text-ok-strong" : "border-app-border bg-app-surface text-fg-secondary hover:bg-app-surface-alt"
                  )}
                >
                  <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-current" />
                  {CONCERN_LABELS[concern]}
                </button>
              ))}
            </div>
          )}
        </header>

        {content ? (
          <div ref={contentRef} role="tabpanel" id={`${id}-content`} aria-labelledby={`${id}-${activeConcern}`} tabIndex={0}
            className="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain px-5 py-5 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-500 sm:px-6"
          >
            <section className="space-y-1 rounded-btn border border-warn-line bg-warn-bg px-3.5 py-3 text-xs leading-5 text-warn-fg">
              <h3 className="text-[13px] font-medium">{content.title}</h3>
              <p>{content.description}</p>
            </section>
            <section>
              <h3 className="text-sm font-semibold">Evidence</h3>
              <dl className="mt-3 space-y-3 text-xs leading-5">
                {content.evidence.map((fact) => (
                  <div key={fact.label} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] gap-x-4">
                    <dt className="text-fg-muted">{fact.label}</dt>
                    <dd className="break-words text-right text-fg">
                      {fact.dateTime ? <time dateTime={fact.dateTime} title={fact.dateTime}>{fact.value}</time> : fact.value}
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
            <section className="border-t border-app-border pt-5">
              <h3 className="text-sm font-semibold">Next steps</h3>
              <ol className="mt-3 list-decimal space-y-2 pl-4 text-xs leading-5 text-fg-secondary">
                {content.nextSteps.map((step) => <li key={step} className="pl-1">{step}</li>)}
              </ol>
            </section>
          </div>
        ) : <div role="status" className="min-h-0 flex-1 overflow-y-auto px-5 py-5 text-sm sm:px-6">No confirmed coverage concerns remain for this resource.</div>}

        <footer className="shrink-0 border-t border-app-border px-5 py-4 sm:px-6">
          {(recheckStatus === "done" || recheckStatus === "error") && (
            <p role="status" className={`mb-3 text-xs ${recheckStatus === "error" ? "text-bad-fg" : "text-fg-secondary"}`}>
              {recheckStatus === "error" ? "Could not recheck this assessment. Try again." : "Assessment refreshed."}
            </p>
          )}
          <div className="flex flex-wrap justify-end gap-3">
            <button type="button" disabled title="No source destination is available"
              className="inline-flex min-h-9 cursor-not-allowed items-center justify-center gap-1 rounded-btn border border-ok-line bg-ok-bg px-4 py-2 text-xs font-medium text-ok-strong opacity-55"
            >Check source <ArrowUpRight aria-hidden="true" className="size-3" /></button>
            <button type="button" disabled={recheckStatus === "checking"} aria-busy={recheckStatus === "checking"}
              onClick={() => void recheck()}
              className="min-h-9 cursor-pointer rounded-btn border border-app-border bg-app-surface px-4 py-2 text-xs font-medium text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-accent-500 disabled:cursor-wait"
            >{recheckStatus === "checking" ? "Rechecking…" : "Recheck"}</button>
          </div>
          <p className="mt-3 text-[11px] leading-4 text-fg-muted">Gaps clear after a successful assessment.</p>
        </footer>
      </div>
    </dialog>
  );
}

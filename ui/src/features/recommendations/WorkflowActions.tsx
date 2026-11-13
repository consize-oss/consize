"use client";
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { apiGet, apiPost } from "@/lib/api-client";
import {
  canApprovePlan,
  runtimeAcceptedSchema,
  runtimeJobSchema,
  runtimePlanSchema,
  terminalJobs,
} from "@/lib/runtime-api";

const jobsSchema = z.object({
  jobs: z
    .array(runtimeJobSchema)
    .nullable()
    .transform((x) => x ?? []),
});
const BUTTON =
  "rounded-btn border border-app-border bg-app-surface px-3 py-2 text-sm text-fg disabled:opacity-50 disabled:cursor-not-allowed focus-visible:outline-2 focus-visible:outline-accent-500";
export function WorkflowActions({
  id,
  eligible,
}: {
  id: string;
  eligible: boolean;
}) {
  const client = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const [command, setCommand] = useState<"execute" | "recover">("execute");
  const dialog = useRef<HTMLDialogElement>(null);
  const jobs = useQuery({
    queryKey: ["workflow-jobs", id],
    queryFn: ({ signal }) => apiGet("/api/jobs", jobsSchema, signal),
    refetchInterval: (query) => {
      const job = query.state.data?.jobs.find(
        (x) => String(x.recommendation_id) === id,
      );
      return job && !terminalJobs.has(job.state) ? 5000 : false;
    },
  });
  const job = jobs.data?.jobs.find((x) => String(x.recommendation_id) === id);
  const plan = useMutation({
    mutationFn: () =>
      apiPost(
        `/api/recommendations/${encodeURIComponent(id)}/plan`,
        runtimePlanSchema,
        undefined,
      ),
    retry: false,
  });
  const execute = useMutation({
    mutationFn: () =>
      apiPost(
        `/api/recommendations/${encodeURIComponent(id)}/${command}`,
        runtimeAcceptedSchema,
        undefined,
        { mode: "approved" },
      ),
    retry: false,
    onSettled: () => client.invalidateQueries({ queryKey: ["workflow-jobs", id] }),
    onSuccess: async (result) => {
      setConfirm(false);
      client.setQueryData(["workflow-jobs", id], { jobs: [result.job] });
      await Promise.all([
        client.invalidateQueries({ queryKey: ["workflow-jobs", id] }),
        client.invalidateQueries({ queryKey: ["recommendations"] }),
        client.invalidateQueries({ queryKey: ["runtime-dashboard"] }),
        client.invalidateQueries({ queryKey: ["audit-events"] }),
      ]);
    },
  });
  useEffect(() => {
    if (confirm) dialog.current?.showModal();
    else dialog.current?.close();
  }, [confirm]);
  const state = job?.state;
  useEffect(() => {
    if (state && terminalJobs.has(state)) {
      void client.invalidateQueries({ queryKey: ["recommendations"] });
      void client.invalidateQueries({ queryKey: ["audit-events"] });
    }
  }, [state, client]);
  const allowed =
    eligible && !job && !jobs.isError && !jobs.isPending && !jobs.isFetching;
  const recoverable =
    job?.state === "manual_intervention" && !jobs.isError && !jobs.isFetching;
  const planned = plan.data?.planned;
  const blocked = !canApprovePlan(planned);
  const error = plan.error ?? execute.error ?? jobs.error;
  return (
    <section
      className="rounded-card border border-app-border bg-app-surface p-5 space-y-3"
      aria-label="Execution workflow"
    >
      <h2 className="font-semibold text-fg">Review and apply</h2>
      {error && (
        <p role="alert" className="text-sm text-bad-fg">
          {error.message}
        </p>
      )}
      {jobs.isError && (
        <button className={BUTTON} onClick={() => void jobs.refetch()}>
          Retry job status
        </button>
      )}
      {job ? (
        <div role="status" className="space-y-1 text-sm text-fg-secondary">
          <p>
            Job {job.id}: <strong>{job.state.replaceAll("_", " ")}</strong>
          </p>
          <p>Deadline: {new Date(job.deadline).toLocaleString()}</p>
          {job.last_error && <p>{job.last_error}</p>}
          {job.verification_result && (
            <p>
              Verification: {job.verification_result.status}{" "}
              {job.verification_result.reasons.join("; ")}
            </p>
          )}
          {job.state === "manual_intervention" && (
            <p>
              Recovery restores the captured original state. The backend refuses
              external drift.
              <button
                className={BUTTON}
                disabled={!recoverable || execute.isPending}
                onClick={() => {
                  setCommand("recover");
                  setConfirm(true);
                }}
              >
                Review and retry recovery
              </button>
            </p>
          )}
        </div>
      ) : (
        <>
          <button
            className={BUTTON}
            disabled={!allowed || plan.isPending || execute.isPending}
            onClick={() => {
              execute.reset();
              plan.mutate();
            }}
          >
            {plan.isPending ? "Planning…" : "Review dry-run plan"}
          </button>
          {!eligible && (
            <p className="text-xs text-fg-muted">
              This recommendation is not eligible for a new execution.
            </p>
          )}
          {planned && (
            <div className="space-y-2 text-sm text-fg-secondary">
              <p>{planned.plan.summary}</p>
              <p>Policy: {planned.policy_decision.decision}</p>
              <ul>
                {planned.policy_decision.reasons.map((x) => (
                  <li key={x}>{x}</li>
                ))}
                {planned.plan.preflight?.map((x) => (
                  <li key={x.id}>
                    {x.id}: {x.status} — {x.message}
                  </li>
                ))}
              </ul>
              <div className="overflow-auto">
                <pre className="text-xs">
                  {JSON.stringify(
                    {
                      current: planned.plan.original_state,
                      proposed: planned.plan.applied_state,
                    },
                    null,
                    2,
                  )}
                </pre>
              </div>
              <button
                className={BUTTON}
                disabled={
                  !allowed ||
                  plan.isPending ||
                  plan.isError ||
                  Boolean(blocked) ||
                  execute.isPending ||
                  planned.plan.preflight?.some((x) => x.status !== "passed")
                }
                onClick={() => {
                  setCommand("execute");
                  setConfirm(true);
                }}
              >
                Approve and apply
              </button>
            </div>
          )}
        </>
      )}
      <dialog
        aria-labelledby="workflow-confirm-title"
        ref={dialog}
        onCancel={() => setConfirm(false)}
        onClose={() => setConfirm(false)}
        className="max-w-lg w-[calc(100%-2rem)] rounded-card border border-app-border bg-app-surface p-6 text-fg backdrop:bg-black/50"
      >
        <h3 id="workflow-confirm-title" className="font-semibold">
          {command === "recover"
            ? "Restore the captured original state?"
            : "Apply this infrastructure change?"}
        </h3>
        <p className="mt-3 text-sm">
          This queues a real change. The backend repeats policy and preflight
          checks, then verifies the result under the configured rollback policy.
        </p>
        {execute.error && (
          <p role="alert" className="mt-3 text-sm text-bad-fg">
            {execute.error.message}
          </p>
        )}
        <div className="mt-5 flex flex-wrap gap-3">
          <button
            autoFocus
            className={BUTTON}
            disabled={execute.isPending}
            onClick={() => setConfirm(false)}
          >
            Cancel
          </button>
          <button
            className={BUTTON}
            disabled={
              execute.isPending ||
              (command === "recover"
                ? !recoverable
                : !allowed || Boolean(blocked))
            }
            onClick={() => execute.mutate()}
          >
            {execute.isPending
              ? "Submitting…"
              : command === "recover"
                ? "Confirm recovery"
                : "Confirm apply"}
          </button>
        </div>
      </dialog>
    </section>
  );
}

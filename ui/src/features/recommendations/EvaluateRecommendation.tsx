"use client";
import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { useResourceList } from "@/features/resources/queries";
import { apiPost } from "@/lib/api-client";
import { runtimeRecommendationSchema } from "@/lib/runtime-api";
const BUTTON =
  "rounded-btn border border-app-border bg-app-surface px-3 py-2 text-xs text-fg disabled:opacity-50";
export function EvaluateRecommendation() {
  const resources = useResourceList();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const evaluate = useMutation({
    mutationFn: () =>
      apiPost(
        `/api/recommendations/generate?${new URLSearchParams({ resource_id: selected })}`,
        z.object({ recommendations: z.array(runtimeRecommendationSchema) }),
      ),
    retry: false,
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["runtime-dashboard"] });
    },
  });
  useEffect(() => {
    if (open) dialog.current?.showModal();
    else dialog.current?.close();
  }, [open]);
  const items =
    resources.data?.resources.filter(
      (x) =>
        x.lifecycle_state === "active" &&
        x.support_status === "fully_supported",
    ) ?? [];
  return (
    <>
      <button
        className={BUTTON}
        disabled={
          resources.isError || resources.isPending || items.length === 0
        }
        title={
          items.length === 0
            ? "Discover supported resources before evaluation"
            : undefined
        }
        onClick={() => {
          evaluate.reset();
          setOpen(true);
        }}
      >
        Evaluate resource
      </button>
      <dialog
        aria-labelledby="evaluation-title"
        ref={dialog}
        onCancel={() => setOpen(false)}
        onClose={() => setOpen(false)}
        className="max-w-lg w-[calc(100%-2rem)] rounded-card border border-app-border bg-app-surface p-6 text-fg backdrop:bg-black/50"
      >
        <h2 id="evaluation-title" className="font-semibold">
          Evaluate a resource
        </h2>
        <label className="block mt-4 text-sm">
          Resource
          <select
            className="mt-2 block w-full border border-app-border bg-app-field p-2"
            value={selected}
            onChange={(e) => {
              setSelected(e.target.value);
              evaluate.reset();
            }}
          >
            <option value="">Select a resource</option>
            {items.map((x) => (
              <option key={x.id} value={x.id}>
                {x.name} · {x.environment}
              </option>
            ))}
          </select>
        </label>
        <p className="mt-2 text-xs text-fg-muted">
          Uses real telemetry. Evaluation creates recommendations; it does not
          apply changes.
        </p>
        {evaluate.error && (
          <p role="alert" className="mt-3 text-sm text-bad-fg">
            {evaluate.error.message}
          </p>
        )}
        {evaluate.data && (
          <p role="status" className="mt-3 text-sm">
            {evaluate.data.recommendations.length} recommendations created.
          </p>
        )}
        <div className="mt-4 flex gap-3">
          <button className={BUTTON} onClick={() => setOpen(false)}>
            Close
          </button>
          <button
            className={BUTTON}
            disabled={!selected || evaluate.isPending || resources.isError}
            onClick={() => evaluate.mutate()}
          >
            {evaluate.isPending ? "Evaluating…" : "Run evaluation"}
          </button>
        </div>
      </dialog>
    </>
  );
}

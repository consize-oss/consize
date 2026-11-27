"use client";

import { useId, useState } from "react";
import { Building2, Check, ChevronDown, ChevronRight, Save } from "lucide-react";
import { Modal, ModalBody, ModalFooter } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";
import { usePluginConfiguration, useSaveConfiguration } from "./api";
import type { Configuration, PluginDetail } from "./schemas";

const BTN =
  "inline-flex h-9 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-btn border px-4 text-xs font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";
const SECONDARY = BTN + " border-app-border bg-app-surface text-fg-secondary hover:bg-app-surface-alt";
const PRIMARY = BTN + " border-emerald-600 bg-emerald-600 text-white hover:bg-emerald-500";
const INPUT =
  "h-9 w-full rounded-md border border-app-border bg-app-bg px-3 text-xs text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";

const messageOf = (error: unknown) =>
  error instanceof ApiError
    ? (error.problem?.detail ?? error.problem?.title ?? error.message)
    : "Something went wrong. Please try again.";

export function ConfigureDialog({
  plugin,
  onClose,
  onSaved,
}: {
  plugin: PluginDetail;
  onClose: () => void;
  onSaved: (detail: PluginDetail) => void;
}) {
  const { data, isPending, isError, error, refetch } = usePluginConfiguration(plugin.id, true);

  return (
    <Modal title={`Configure ${plugin.name}`} onClose={onClose}>
      {isPending ? (
        <>
          <ModalBody>
            <div role="status" aria-label="Loading configuration" className="space-y-3 pt-2">
              <div className="h-4 w-64 animate-pulse rounded bg-app-surface-alt motion-reduce:animate-none" />
              <div className="h-44 animate-pulse rounded bg-app-surface-alt motion-reduce:animate-none" />
            </div>
          </ModalBody>
          <FooterClose onClose={onClose} />
        </>
      ) : isError ? (
        <>
          <ModalBody>
            <div role="alert" className="pt-2">
              <p className="text-sm font-medium text-fg">Couldn&apos;t load the configuration</p>
              <p className="mt-1 text-xs text-fg-muted">{messageOf(error)}</p>
              <button type="button" onClick={() => void refetch()} className={SECONDARY + " mt-3 h-8"}>
                Retry
              </button>
            </div>
          </ModalBody>
          <FooterClose onClose={onClose} />
        </>
      ) : (
        <ConfigureForm plugin={plugin} config={data} onClose={onClose} onSaved={onSaved} />
      )}
    </Modal>
  );
}

function FooterClose({ onClose }: { onClose: () => void }) {
  return (
    <ModalFooter>
      <button type="button" onClick={onClose} className={SECONDARY}>Later</button>
    </ModalFooter>
  );
}

function Row({ label, htmlFor, labelId, children }: { label: string; htmlFor?: string; labelId?: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-2 py-3 sm:grid-cols-[150px_1fr] sm:gap-6">
      {htmlFor ? (
        <label htmlFor={htmlFor} className="pt-2.5 text-xs font-medium text-fg">{label}</label>
      ) : (
        <span id={labelId} className="pt-0.5 text-xs font-medium text-fg">{label}</span>
      )}
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function ConfigureForm({
  plugin,
  config,
  onClose,
  onSaved,
}: {
  plugin: PluginDetail;
  config: Configuration;
  onClose: () => void;
  onSaved: (detail: PluginDetail) => void;
}) {
  const save = useSaveConfiguration(plugin.id);
  const ids = useId();
  const available = new Set(config.accounts.flatMap((a) => a.repositories.map((r) => r.id)));
  // Saved repositories that are no longer reachable can't be kept silently.
  const unavailable = config.values.repository_ids.filter((id) => !available.has(id));

  const [selected, setSelected] = useState(() => new Set(config.values.repository_ids.filter((id) => available.has(id))));
  const [name, setName] = useState(config.values.name || "Default");
  const [baseBranch, setBaseBranch] = useState(config.values.base_branch);
  const [path, setPath] = useState(config.values.infrastructure_path || "/");
  const [mapping, setMapping] = useState(config.values.resource_mapping);
  const [optionalOpen, setOptionalOpen] = useState(false);
  const [needsRepo, setNeedsRepo] = useState(false);
  const busy = save.isPending;

  function toggle(id: string) {
    setNeedsRepo(false);
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function submit(enable: boolean) {
    // Enabling a write-capable plugin with nothing selected isn't valid; saving
    // it disabled is allowed so a half-finished setup isn't lost.
    if (enable && selected.size === 0) {
      setNeedsRepo(true);
      return;
    }
    save.mutate(
      {
        repository_ids: [...selected],
        name: name.trim() || "Default",
        base_branch: baseBranch.trim(),
        infrastructure_path: path.trim() || "/",
        resource_mapping: mapping.trim(),
        enable,
      },
      { onSuccess: onSaved },
    );
  }

  const repoLabelId = `${ids}-repos`;
  const repoHelpId = `${ids}-repos-help`;

  return (
    <form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        submit(true);
      }}
      className="flex min-h-0 flex-1 flex-col"
    >
      <ModalBody>
        <p className="pb-1 text-xs text-fg-secondary">Complete the required fields to enable this plugin.</p>

        <Row label="Repositories *" labelId={repoLabelId}>
          <div
            role="group"
            aria-labelledby={repoLabelId}
            aria-describedby={repoHelpId}
            aria-invalid={needsRepo}
            className={"overflow-hidden rounded-md border " + (needsRepo ? "border-bad-line" : "border-app-border")}
          >
            <div className="flex items-center justify-between bg-app-bg px-3 py-3 text-[11px]">
              <span className="text-fg-muted">Connected accounts</span>
              {config.connect_url ? (
                <a
                  href={config.connect_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-medium text-ok-strong hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
                >
                  Connect account
                </a>
              ) : (
                <span className="text-fg-muted" title="Connecting accounts isn't available here yet">Connect account</span>
              )}
            </div>
            {config.accounts.length === 0 && (
              <p className="border-t border-app-border px-3 py-4 text-xs text-fg-muted">
                No accounts are connected yet. Connect an account to choose repositories.
              </p>
            )}
            {config.accounts.map((account) => (
              <fieldset key={account.id} aria-labelledby={`${ids}-acct-${account.id}`} className="border-t border-app-border px-3 py-3">
                <p id={`${ids}-acct-${account.id}`} className="flex items-center gap-1.5 text-[11px] text-fg-muted">
                  <Building2 aria-hidden="true" className="h-3.5 w-3.5" />
                  {account.name}
                </p>
                {account.repositories.map((repo) => (
                  <label key={repo.id} className="mt-3 flex cursor-pointer items-center gap-2.5 text-xs text-fg">
                    <input
                      type="checkbox"
                      checked={selected.has(repo.id)}
                      disabled={busy}
                      onChange={() => toggle(repo.id)}
                      className="h-3.5 w-3.5 cursor-pointer accent-emerald-600"
                    />
                    {repo.full_name}
                  </label>
                ))}
              </fieldset>
            ))}
          </div>
          <p id={repoHelpId} className="mt-2 text-[11px] leading-relaxed text-fg-muted">
            Select repositories from one or more GitHub App installations.
          </p>
          {needsRepo && (
            <p role="alert" className="mt-2 text-xs text-bad-fg">Select at least one repository to enable this plugin.</p>
          )}
          {unavailable.length > 0 && (
            <p role="status" className="mt-2 text-xs text-warn-fg">
              {unavailable.length} previously selected {unavailable.length === 1 ? "repository is" : "repositories are"} no
              longer available and will be removed when you save.
            </p>
          )}
        </Row>

        <div className="mt-2 border-t border-app-border pt-4">
          <button
            type="button"
            aria-expanded={optionalOpen}
            aria-controls={`${ids}-optional`}
            onClick={() => setOptionalOpen((open) => !open)}
            className="flex cursor-pointer items-center gap-1.5 rounded text-xs text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
          >
            {optionalOpen ? <ChevronDown aria-hidden="true" className="h-3.5 w-3.5" /> : <ChevronRight aria-hidden="true" className="h-3.5 w-3.5" />}
            Optional settings
          </button>

          {optionalOpen && (
            <div id={`${ids}-optional`} className="mt-1">
              <Row label="Configuration name" htmlFor={`${ids}-name`}>
                <input id={`${ids}-name`} value={name} disabled={busy} onChange={(e) => setName(e.target.value)} className={INPUT} />
              </Row>
              <Row label="Base branch" htmlFor={`${ids}-branch`}>
                <input id={`${ids}-branch`} value={baseBranch} disabled={busy} onChange={(e) => setBaseBranch(e.target.value)} className={INPUT} aria-describedby={`${ids}-branch-help`} />
                <p id={`${ids}-branch-help`} className="mt-2 text-[11px] text-fg-muted">Leave empty to use each repository&apos;s default branch.</p>
              </Row>
              <Row label="Infrastructure path" htmlFor={`${ids}-path`}>
                <input id={`${ids}-path`} value={path} disabled={busy} onChange={(e) => setPath(e.target.value)} className={INPUT} />
              </Row>
              <Row label="Resource mapping" htmlFor={`${ids}-mapping`}>
                <input id={`${ids}-mapping`} value={mapping} disabled={busy} onChange={(e) => setMapping(e.target.value)} className={INPUT} aria-describedby={`${ids}-mapping-help`} />
                <p id={`${ids}-mapping-help`} className="mt-2 text-[11px] text-fg-muted">Configure routing before creating pull requests.</p>
              </Row>
            </div>
          )}
        </div>

        {save.isError && (
          <p role="alert" className="mt-3 rounded-btn border border-bad-line bg-bad-bg px-3 py-2 text-xs text-bad-fg">
            {messageOf(save.error)}
          </p>
        )}
      </ModalBody>

      <ModalFooter>
        <button type="button" onClick={onClose} disabled={busy} className={SECONDARY}>Later</button>
        <div className="flex items-center gap-2.5">
          <button type="button" onClick={() => submit(false)} disabled={busy} className={SECONDARY}>
            <Save aria-hidden="true" className="h-4 w-4" />
            Save disabled
          </button>
          <button type="submit" disabled={busy} aria-busy={busy} className={PRIMARY}>
            <Check aria-hidden="true" className="h-4 w-4" />
            {busy ? "Saving..." : "Save & enable"}
          </button>
        </div>
      </ModalFooter>
    </form>
  );
}
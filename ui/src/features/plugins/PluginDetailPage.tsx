"use client";

import { useState } from "react";
import Link from "next/link";
import { BookOpen, ChevronRight, Eye, Plus, Settings, SquarePen, ArrowUpRight } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { ConfigureDialog } from "./ConfigureDialog";
import { PluginLogo } from "./PluginLogo";
import { useInstallPlugin, usePlugin } from "./api";
import { CAPABILITY_LABELS, type PluginDetail } from "./schemas";
import { INSTALLATION_STATUS, TONE_PILL } from "./status";

const PRIMARY =
  "inline-flex h-10 shrink-0 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-btn border border-emerald-600 bg-emerald-600 px-4 text-xs font-medium text-white hover:bg-emerald-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";

export function PluginDetailPage({ id }: { id: string }) {
  const { data: plugin, isPending, isError, error, refetch } = usePlugin(id);

  if (isPending) {
    return <div role="status" aria-label="Loading plugin" className="h-96 animate-pulse rounded-card border border-app-border bg-app-surface-alt motion-reduce:animate-none" />;
  }
  if (isError) {
    const missing = error instanceof ApiError && error.status === 404;
    return (
      <div role="alert" className="rounded-card border border-app-border bg-app-surface p-6">
        <p className="text-sm font-medium text-fg">{missing ? "This plugin isn't in the catalog" : "Couldn't load this plugin"}</p>
        <p className="mt-1 text-xs text-fg-muted">
          {missing ? "It may have been removed, or the link is wrong." : error instanceof Error ? error.message : "The plugin service is unavailable."}
        </p>
        <div className="mt-3 flex gap-2">
          <Link href="/plugins?tab=catalog" className="inline-flex h-8 items-center rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt">
            Back to the catalog
          </Link>
          {!missing && (
            <button type="button" onClick={() => void refetch()} className="h-8 cursor-pointer rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt">
              Retry
            </button>
          )}
        </div>
      </div>
    );
  }
  return <PluginDetail plugin={plugin} />;
}

function PluginDetail({ plugin }: { plugin: PluginDetail }) {
  const install = useInstallPlugin(plugin.id);
  const [configuring, setConfiguring] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const installation = plugin.installation;
  const status = installation ? INSTALLATION_STATUS[installation.status] : undefined;
  const installError =
    install.error instanceof ApiError
      ? (install.error.problem?.detail ?? install.error.problem?.title ?? install.error.message)
      : install.isError
        ? "Could not install this plugin. Please try again."
        : null;

  function doInstall() {
    setNotice(null);
    install.mutate(undefined, {
      // Installing leaves required settings, so go straight to them; "Later"
      // keeps the plugin installed-but-unconfigured.
      onSuccess: (detail) => {
        if (detail.configurable && detail.installation?.status === "configuration_required") setConfiguring(true);
      },
    });
  }

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-xs text-fg-muted">
        <ol className="flex items-center gap-3">
          <li><Link href="/plugins" className="hover:text-fg hover:underline">Plugins</Link></li>
          <li aria-hidden="true"><ChevronRight className="h-3.5 w-3.5" /></li>
          <li><Link href="/plugins?tab=catalog" className="hover:text-fg hover:underline">Catalog</Link></li>
          <li aria-hidden="true"><ChevronRight className="h-3.5 w-3.5" /></li>
          <li aria-current="page" className="text-fg-secondary">{plugin.name}</li>
        </ol>
      </nav>

      <header className="flex items-start justify-between gap-6">
        <div className="flex items-center gap-3">
          <PluginLogo brand={plugin.brand} size="lg" label={plugin.name} />
          <div>
            <h1 className="text-2xl font-semibold tracking-tight text-fg">{plugin.name}</h1>
            <p className="mt-1.5 text-[11px] text-fg-muted">{plugin.publisher.name} · {plugin.version}</p>
          </div>
        </div>

        {!installation ? (
          <button type="button" onClick={doInstall} disabled={install.isPending || plugin.install_blocker !== null} aria-busy={install.isPending} className={PRIMARY}>
            <Plus aria-hidden="true" className="h-4 w-4" />
            {install.isPending ? "Installing..." : "Install"}
          </button>
        ) : plugin.configurable ? (
          <button type="button" onClick={() => setConfiguring(true)} className={PRIMARY}>
            <Settings aria-hidden="true" className="h-4 w-4" />
            Configure
          </button>
        ) : null}
      </header>

      <p className="text-[13px] text-fg-secondary">{plugin.description}</p>

      {!installation && plugin.install_blocker && (
        <p role="note" className="rounded-btn border border-warn-line bg-warn-bg px-3.5 py-3 text-xs text-warn-fg">
          {plugin.install_blocker}
        </p>
      )}
      {installError && (
        <p role="alert" className="rounded-btn border border-bad-line bg-bad-bg px-3.5 py-3 text-xs text-bad-fg">{installError}</p>
      )}
      {installation && status && (
        <p className={"rounded-btn border px-3.5 py-3 text-xs " + (status.tone === "ok" || status.tone === "neutral" ? "border-app-border bg-app-surface text-fg" : TONE_PILL[status.tone])}>
          Installed · {status.label}
          {installation.status_reason && <span className="text-fg-muted"> · {installation.status_reason}</span>}
        </p>
      )}
      {notice && <p role="status" className="text-xs text-ok-strong">{notice}</p>}

      <section className="grid grid-cols-1 gap-2 border-t border-app-border py-6 md:grid-cols-[160px_1fr] md:gap-7">
        <h2 className="text-sm font-semibold text-fg">Overview</h2>
        <div className="max-w-[700px] text-xs leading-relaxed">
          <p className="text-fg">{plugin.overview}</p>
          {plugin.overview_note && <p className="mt-2 text-fg-muted">{plugin.overview_note}</p>}
          <p className="mt-3 text-fg-muted">{plugin.capabilities.map((c) => CAPABILITY_LABELS[c]).join(" · ")}</p>
        </div>
      </section>

      <section className="grid grid-cols-1 gap-2 border-t border-app-border py-6 md:grid-cols-[160px_1fr] md:gap-7">
        <h2 className="text-sm font-semibold text-fg">Permissions</h2>
        <ul className="max-w-[700px] space-y-5 text-xs">
          {plugin.permissions.map((perm) => {
            const Icon = perm.access === "write" ? SquarePen : Eye;
            return (
              <li key={perm.id}>
                <p className="flex items-center gap-2.5 text-fg">
                  <Icon aria-hidden="true" className="h-4 w-4 text-fg-muted" />
                  {perm.title}
                  <span className="sr-only"> ({perm.access === "write" ? "can change things" : "read only"})</span>
                </p>
                <p className="mt-1 text-fg-muted">{perm.description}</p>
              </li>
            );
          })}
          {plugin.permissions.length === 0 && <li className="text-fg-muted">This plugin declares no permissions.</li>}
        </ul>
      </section>

      {plugin.documentation_url && (
        <a
          href={plugin.documentation_url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex w-fit items-center gap-1.5 rounded text-xs text-ok-strong hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
        >
          <BookOpen aria-hidden="true" className="h-3.5 w-3.5" />
          Documentation
          <ArrowUpRight aria-hidden="true" className="h-3.5 w-3.5" />
          <span className="sr-only">(opens in a new tab)</span>
        </a>
      )}

      {configuring && (
        <ConfigureDialog
          plugin={plugin}
          onClose={() => setConfiguring(false)}
          onSaved={(detail) => {
            setConfiguring(false);
            setNotice(
              detail.installation?.status === "disabled"
                ? "Configuration saved. The plugin is installed but disabled."
                : "Configuration saved.",
            );
          }}
        />
      )}
    </>
  );
}
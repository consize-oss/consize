import { BarChart3, Bell, CircleDollarSign, Gauge, GitPullRequest } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { Plugin, PluginKind } from "@/schemas";

export type StatusTone = "ok" | "warn" | "bad" | "neutral";

// One definition of "what state is this plugin in", shared by the Overview
// strip and the Plugins screen so the two can never disagree. Order matters:
// a broken connection outranks a pending review, which outranks "healthy".
export function pluginStatus(p: Pick<Plugin, "health" | "attention" | "engine_state">): {
  label: string;
  tone: StatusTone;
} {
  if (p.health === "unavailable") return { label: "Connection failing", tone: "bad" };
  if (p.health === "setup_required") return { label: "Setup required", tone: "neutral" };
  if (p.attention.includes("permission_review")) return { label: "Permission review", tone: "warn" };
  if (p.attention.includes("credential_expiring")) return { label: "Credential expiring", tone: "warn" };
  if (p.health === "degraded") return { label: "Degraded", tone: "warn" };
  if (p.engine_state === "active") return { label: "Active engine", tone: "ok" };
  if (p.engine_state === "standby") return { label: "Standby", tone: "neutral" };
  return { label: "Healthy", tone: "ok" };
}

export const SEVERITY: Record<StatusTone, number> = { bad: 0, warn: 1, neutral: 2, ok: 3 };

export const PILL: Record<StatusTone, string> = {
  ok: "bg-ok-bg text-ok-fg border-ok-line",
  warn: "bg-warn-bg text-warn-fg border-warn-line",
  bad: "bg-bad-bg text-bad-fg border-bad-line",
  neutral: "bg-app-surface-alt text-fg-secondary border-app-border",
};

export const DOT: Record<StatusTone, string> = {
  ok: "bg-emerald-400",
  warn: "bg-amber-400",
  bad: "bg-rose-400",
  neutral: "bg-slate-400",
};

export const FACT_TEXT = {
  default: "text-fg-secondary",
  ok: "text-ok-strong",
  warn: "text-warn-fg",
  bad: "text-bad-fg",
} as const;

export const KIND: Record<PluginKind, { label: string; chip: string; tile: string; icon: LucideIcon }> = {
  evidence: {
    label: "Evidence",
    chip: PILL.ok,
    tile: "bg-violet-500/10 border-violet-500/25 text-violet-500",
    icon: BarChart3,
  },
  cost: { label: "Cost", chip: "bg-info-bg text-info-fg border-info-line", tile: "bg-info-bg border-info-line text-info-fg", icon: CircleDollarSign },
  action: { label: "Action", chip: PILL.warn, tile: "bg-warn-bg border-warn-line text-warn-fg", icon: GitPullRequest },
  algorithm: { label: "Algorithm", chip: PILL.ok, tile: "bg-ok-bg border-ok-line text-ok-strong", icon: Gauge },
  notification: { label: "Notification", chip: PILL.neutral, tile: "bg-app-surface-alt border-app-border text-fg-secondary", icon: Bell },
};
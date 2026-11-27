import type { Installation, Plugin } from "./schemas";

export type Tone = "ok" | "warn" | "bad" | "neutral";

// One place that says what an installation's state means, shared by the
// Installed table, the catalog cards, the detail page and the Overview strip.
export const INSTALLATION_STATUS: Record<Installation["status"], { label: string; tone: Tone; rank: number }> = {
  failing: { label: "Failing", tone: "bad", rank: 0 },
  degraded: { label: "Degraded", tone: "warn", rank: 1 },
  configuration_required: { label: "Configuration required", tone: "warn", rank: 2 },
  disabled: { label: "Disabled", tone: "neutral", rank: 3 },
  connected: { label: "Connected", tone: "ok", rank: 4 },
  healthy: { label: "Healthy", tone: "ok", rank: 4 },
};

export const TONE_TEXT: Record<Tone, string> = {
  ok: "text-ok-strong",
  warn: "text-warn-fg",
  bad: "text-bad-fg",
  neutral: "text-fg-secondary",
};

export const TONE_PILL: Record<Tone, string> = {
  ok: "bg-ok-bg text-ok-fg border-ok-line",
  warn: "bg-warn-bg text-warn-fg border-warn-line",
  bad: "bg-bad-bg text-bad-fg border-bad-line",
  neutral: "bg-app-surface-alt text-fg-secondary border-app-border",
};

export const AVAILABILITY_LABELS: Record<Plugin["availability"], string> = {
  available: "Available",
  built_in: "Built-in",
  requires_upgrade: "Requires upgrade",
  verification_required: "Verification required",
};

// The right-hand label on a catalog card.
export function cardState(plugin: Plugin): { label: string; tone: Tone } {
  const inst = plugin.installation;
  if (!inst) return { label: AVAILABILITY_LABELS[plugin.availability], tone: "neutral" };
  const status = INSTALLATION_STATUS[inst.status];
  // A healthy install reads simply "Installed"; problems say what they are.
  return status.tone === "ok" ? { label: "Installed", tone: "neutral" } : { label: status.label, tone: status.tone };
}
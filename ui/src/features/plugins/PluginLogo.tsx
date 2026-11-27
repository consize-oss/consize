import { CircleDollarSign, Puzzle, Webhook } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { Brand } from "./schemas";
import { LOGO_PATHS } from "./logoPaths";

// Brand logos sit bare; plugins without one get the design's tinted tile.
const BRAND_COLOR: Partial<Record<Brand, string>> = {
  prometheus: "#E6522C",
  kubernetes: "#326CE5",
  github: "currentColor",
  datadog: "#632CA6",
};

const TILE: Partial<Record<Brand, { icon: LucideIcon; classes: string }>> = {
  pricing: { icon: CircleDollarSign, classes: "border-info-line bg-info-bg text-info-fg" },
  webhook: { icon: Webhook, classes: "border-rose-500/25 bg-rose-500/10 text-rose-500" },
  generic: { icon: Puzzle, classes: "border-app-border bg-app-subcard text-fg-secondary" },
};

export function PluginLogo({ brand, size = "md", label }: { brand: Brand; size?: "md" | "lg"; label: string }) {
  const box = size === "lg" ? "h-10 w-10" : "h-8 w-8";
  const path = brand in LOGO_PATHS ? LOGO_PATHS[brand as keyof typeof LOGO_PATHS] : undefined;

  if (path) {
    return (
      <span className={`inline-grid shrink-0 place-items-center text-fg ${box}`} role="img" aria-label={`${label} logo`}>
        <svg viewBox="0 0 24 24" aria-hidden="true" className={size === "lg" ? "h-8 w-8" : "h-6 w-6"} fill={BRAND_COLOR[brand]}>
          <path d={path} />
        </svg>
      </span>
    );
  }

  const tile = TILE[brand] ?? TILE.generic!;
  const Icon = tile.icon;
  return (
    <span aria-hidden="true" className={`inline-grid shrink-0 place-items-center rounded-btn border ${box} ${tile.classes}`}>
      <Icon className={size === "lg" ? "h-5 w-5" : "h-4 w-4"} />
    </span>
  );
}
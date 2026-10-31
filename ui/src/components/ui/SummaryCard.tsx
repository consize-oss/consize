import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

export type SummaryCardTone = "neutral" | "success" | "warning" | "danger" | "info";

export type SummaryCardProps = {
  title: string;
  icon: LucideIcon;
  value: string | number;
  valueNote?: ReactNode;
  description: ReactNode;
  iconTone?: SummaryCardTone;
  descriptionTone?: SummaryCardTone;
};

const ICON_TONES: Record<SummaryCardTone, string> = {
  neutral: "border-app-border bg-app-surface-alt text-fg-secondary",
  success: "border-ok-line bg-ok-bg text-ok-strong",
  warning: "border-warn-line bg-warn-bg text-warn-fg",
  danger: "border-bad-line bg-bad-bg text-bad-fg",
  info: "border-info-line bg-info-bg text-info-fg",
};

const DESCRIPTION_TONES: Record<SummaryCardTone, string> = {
  neutral: "text-fg-muted",
  success: "text-ok-strong",
  warning: "text-warn-fg",
  danger: "text-bad-fg",
  info: "text-info-fg",
};

// Display only: each feature supplies its own values and supporting information.
export function SummaryCard({
  title,
  icon: Icon,
  value,
  valueNote,
  description,
  iconTone = "neutral",
  descriptionTone = "neutral",
}: SummaryCardProps) {
  return (
    <article
      aria-label={title}
      className="flex h-full min-w-0 flex-col rounded-card border border-app-border bg-app-surface p-4 shadow-card"
    >
      <div className="flex items-start justify-between gap-3">
        <p className="min-w-0 text-xs font-medium text-fg-secondary">{title}</p>
        <span
          aria-hidden="true"
          className={"inline-flex h-6 w-6 shrink-0 items-center justify-center rounded border " + ICON_TONES[iconTone]}
        >
          <Icon className="h-3.5 w-3.5" />
        </span>
      </div>

      <div className="mb-3 mt-5 flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <p className="break-words text-2xl font-semibold tracking-tight text-fg font-num">{value}</p>
        {valueNote != null && (
          <span className="text-xs text-fg-muted font-num">{valueNote}</span>
        )}
      </div>

      <div
        className={"mt-auto border-t border-app-border-subtle pt-2.5 text-[11px] leading-4 " + DESCRIPTION_TONES[descriptionTone]}
      >
        {description}
      </div>
    </article>
  );
}

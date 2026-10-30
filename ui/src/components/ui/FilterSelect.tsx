"use client";

import { useId } from "react";
import { ChevronDown } from "lucide-react";
import { titleCase } from "@/lib/format";

type FilterSelectProps = {
  label: string;
  allLabel: string;
  value: string;
  options: readonly string[];
  onChange?: (value: string) => void;
  formatOption?: (value: string) => string;
  disabled?: boolean;
};

export function FilterSelect({
  label,
  allLabel,
  value,
  options,
  onChange,
  formatOption = titleCase,
  disabled = false,
}: FilterSelectProps) {
  const id = useId();
  // Preserve a bookmarked value even while its options are loading.
  const availableOptions = value && !options.includes(value) ? [...options, value] : options;

  return (
    <div className="relative max-w-full">
      <label htmlFor={id} className="sr-only">{label}</label>
      <select
        id={id}
        value={value}
        disabled={disabled}
        onChange={onChange ? (event) => onChange(event.target.value) : undefined}
        className="h-8 max-w-full cursor-pointer appearance-none rounded-md border border-app-border bg-app-bg pl-3 pr-7 text-xs text-fg-secondary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
      >
        <option value="">{allLabel}</option>
        {availableOptions.map((option) => (
          <option key={option} value={option}>{formatOption(option)}</option>
        ))}
      </select>
      <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-2 top-1/2 h-3 w-3 -translate-y-1/2 text-fg-muted" />
    </div>
  );
}

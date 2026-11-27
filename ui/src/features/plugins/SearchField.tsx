"use client";

import { useEffect, useRef, useState } from "react";
import { Search } from "lucide-react";

// Search that applies after a short pause and follows external changes
// (Reset, Back) without fighting the user's typing.
export function SearchField({
  value,
  onChange,
  placeholder,
  label,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  label: string;
}) {
  const [draft, setDraft] = useState(value);
  const [applied, setApplied] = useState(value);
  const latest = useRef(onChange);
  latest.current = onChange;

  if (applied !== value) {
    setApplied(value);
    setDraft(value);
  }

  useEffect(() => {
    if (draft === value) return;
    const timer = setTimeout(() => latest.current(draft), 250);
    return () => clearTimeout(timer);
  }, [draft, value]);

  return (
    <div className="relative min-w-0 flex-1">
      <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-muted" />
      <input
        type="search"
        value={draft}
        aria-label={label}
        placeholder={placeholder}
        onChange={(event) => setDraft(event.target.value)}
        className="h-10 w-full rounded-md border border-app-border bg-app-bg pl-9 pr-3 text-xs text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
      />
    </div>
  );
}
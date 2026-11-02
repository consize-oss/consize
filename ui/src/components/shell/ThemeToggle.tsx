"use client";

import { useSyncExternalStore } from "react";
import { Moon, Sun } from "lucide-react";

type Theme = "dark" | "light";

// The inline script in layout.tsx has already applied the right theme
// before paint (saved choice, else the system preference). This only reads
// it back so the icon matches, and persists explicit changes.
function subscribe(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => observer.disconnect();
}
const getTheme = (): Theme => document.documentElement.dataset.theme === "light" ? "light" : "dark";
const getServerTheme = (): Theme => "dark";

export function ThemeToggle() {
  const theme = useSyncExternalStore(subscribe, getTheme, getServerTheme);

  function toggle() {
    const next: Theme = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    window.localStorage.setItem("consize-theme", next);
  }

  // The icon shows the action available (PRD §5.1): sun in dark, moon in light.
  const label = theme === "dark" ? "Switch to light theme" : "Switch to dark theme";
  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={label}
      title={label}
      className="relative w-8 h-8 flex items-center justify-center bg-app-surface border border-app-border hover:border-emerald-500/50 rounded-btn text-fg-secondary hover:text-ok-strong focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500 transition-colors"
    >
      {theme === "dark" ? <Sun className="w-4 h-4" aria-hidden /> : <Moon className="w-4 h-4" aria-hidden />}
    </button>
  );
}
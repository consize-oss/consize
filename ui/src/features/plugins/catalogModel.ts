import type { Capability, Plugin } from "./schemas";

export type CatalogFilters = {
  q: string;
  capability: Capability | "";
  publisher: string;
  compatibleOnly: boolean;
};

function matchesText(p: Plugin, q: string) {
  const terms = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const hay = [p.name, p.description, p.publisher.name, p.package_label].join(" ").toLowerCase();
  return terms.every((t) => hay.includes(t));
}

export function filterCatalog(plugins: readonly Plugin[], f: CatalogFilters) {
  return plugins.filter(
    (p) =>
      (!f.capability || p.capabilities.includes(f.capability)) &&
      (!f.publisher || p.publisher.name === f.publisher) &&
      // "Compatible" = works with this Consize version; an unverified package
      // is a trust question, not a compatibility one, so it stays visible.
      (!f.compatibleOnly || p.availability !== "requires_upgrade") &&
      matchesText(p, f.q),
  );
}

export const installedOf = (plugins: readonly Plugin[]) => plugins.filter((p) => p.installation !== null);

export function filterInstalled(plugins: readonly Plugin[], q: string) {
  return installedOf(plugins).filter((p) => matchesText(p, q));
}

export const publishersOf = (plugins: readonly Plugin[]) => [...new Set(plugins.map((p) => p.publisher.name))].sort();
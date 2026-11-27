import { Suspense } from "react";
import { PluginsPage } from "@/features/plugins/PluginsPage";

// Tab, filters and search live in the URL, so the client page sits in Suspense
// (required for useSearchParams).
export default function Page() {
  return (
    <main className="px-6 pt-4 pb-10 space-y-4 max-w-[1680px] mx-auto">
      <Suspense fallback={<div className="h-96 animate-pulse rounded-card border border-app-border bg-app-surface-alt" />}>
        <PluginsPage />
      </Suspense>
    </main>
  );
}
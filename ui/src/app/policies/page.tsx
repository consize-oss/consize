import { Suspense } from "react";
import { PoliciesScreen } from "@/features/policies/PoliciesScreen";

// Filters, search and paging live in the URL, so the client screen sits
// inside Suspense (required for useSearchParams).
export default function PoliciesPage() {
  return (
    <main className="px-6 pt-4 pb-10 space-y-4 max-w-[1680px] mx-auto">
      <Suspense fallback={<div className="h-96 animate-pulse rounded-card border border-app-border bg-app-surface" />}>
        <PoliciesScreen />
      </Suspense>
    </main>
  );
}
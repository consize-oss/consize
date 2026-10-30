import { Suspense } from "react";
import { ResourcesScreen } from "@/features/resources/ResourcesScreen";

export default function Page() {
  return (
    <main className="mx-auto max-w-[1680px] space-y-4 px-6 pb-10 pt-4">
      <Suspense fallback={
        <div role="status" aria-label="Loading resources page" className="h-96 animate-pulse rounded-card border border-app-border bg-app-surface motion-reduce:animate-none">
          <span className="sr-only">Loading resources...</span>
        </div>
      }>
        <ResourcesScreen />
      </Suspense>
    </main>
  );
}

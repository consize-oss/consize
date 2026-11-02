// import { ComingSoon } from "@/components/shell/ComingSoon";

// export default function Page() {
//   return <ComingSoon title="Plugins" />;
// }
import { Suspense } from "react";
import { PluginsPage } from "@/features/plugins/PluginsPage";

// Filters, search, and the selected plugin live in the URL, so the client
// content sits inside Suspense (required for useSearchParams).
export default function Page() {
  return (
    <main className="px-6 pt-4 pb-10 space-y-4 max-w-[1680px] mx-auto">
      <Suspense fallback={<div className="h-96 animate-pulse rounded-card border border-app-border bg-app-surface" />}>
        <PluginsPage />
      </Suspense>
    </main>
  );
}
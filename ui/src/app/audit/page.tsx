import { AuditLog } from "@/features/overview/AuditLog";
export default function Page() {
  return <main className="px-6 pt-4 pb-10 space-y-4 max-w-[1680px] mx-auto"><h1 className="text-2xl font-semibold text-fg">Audit</h1><p className="text-sm text-fg-muted">Recorded action and verification activity. Full domain history and export are available through the audit API.</p><AuditLog /></main>;
}

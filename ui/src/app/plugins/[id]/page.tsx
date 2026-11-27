import { PluginDetailPage } from "@/features/plugins/PluginDetailPage";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <main className="px-6 pt-4 pb-10 space-y-4 max-w-[1680px] mx-auto">
      <PluginDetailPage id={id} />
    </main>
  );
}
// Page header shared by the list screens (Resources layout, generalized).

export const HEADER_ACTION =
  "inline-flex h-8 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-btn border px-3 text-xs font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";
export const HEADER_ICON_BUTTON =
  "inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-btn border border-app-border bg-app-surface text-fg-secondary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";

export function PageHeader({
  title,
  description,
  children,
}: {
  title: string;
  description: React.ReactNode;
  children?: React.ReactNode;
}) {
  return (
    <header className="flex flex-col gap-3 border-b border-app-border pb-3 lg:flex-row lg:items-center lg:justify-between">
      <div className="min-w-0 flex-1 space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">{title}</h1>
        <div className="text-xs leading-4 text-fg-secondary">{description}</div>
      </div>
      {children && <div className="space-y-2 lg:shrink-0">{children}</div>}
    </header>
  );
}
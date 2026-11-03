import type { Metadata } from "next";
import { Suspense } from "react";
import "@fontsource-variable/inter";
import "@fontsource-variable/geist-mono";
import "./globals.css";
import { Providers } from "./providers";
import { Sidebar } from "@/components/shell/Sidebar";
import { Topbar } from "@/components/shell/Topbar";

export const metadata: Metadata = {
  title: "Consize — Control Plane",
  description: "Policy-governed infrastructure optimization dashboard",
};

// Runs before first paint: saved choice, else the system preference
// (PRD §5.1), else dark. Doing this in an effect would flash the wrong theme.
const THEME_INIT_SCRIPT = `
  try {
    var t = localStorage.getItem('consize-theme');
    if (t !== 'light' && t !== 'dark') {
      t = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }
    document.documentElement.dataset.theme = t;
  } catch (e) {}
`;

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const ScenarioSwitcher = process.env.NODE_ENV === "development" && process.env.NEXT_PUBLIC_USE_MOCKS === "true"
    ? (await import("@/components/dev/ScenarioSwitcher")).ScenarioSwitcher
    : null;
  return (
    <html data-theme="dark" lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body className="bg-app-bg text-fg font-sans antialiased selection:bg-ok-bg-strong selection:text-ok-fg">
        <Providers>
          {/* Sidebar and Topbar read the URL (scope), which Next requires to
              sit inside Suspense. The fallbacks keep the same footprint so the
              layout doesn't jump. */}
          <Suspense
            fallback={
              <aside className="fixed left-0 top-0 h-dvh w-[var(--sidebar-width)] bg-app-sidebar border-r border-app-border z-50" />
            }
          >
            <Sidebar />
          </Suspense>
          <Suspense
            fallback={
              <header className="fixed top-0 left-[var(--sidebar-width)] right-0 h-14 bg-app-sidebar border-b border-app-border z-40" />
            }
          >
            <Topbar />
          </Suspense>
          <div className="pl-[var(--sidebar-width)] pt-14 min-h-screen">{children}</div>
          {ScenarioSwitcher && (
            <div className="fixed bottom-3 right-3 z-50">
              <ScenarioSwitcher />
            </div>
          )}
        </Providers>
      </body>
    </html>
  );
}

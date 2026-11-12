"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";

// URL-backed filter state shared by list pages. Filter changes replace the
// history entry and reset to page 1; page changes push, so Back walks pages.
export function useUrlFilters() {
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();

  function update(changes: Record<string, string>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    params.delete("page");
    params.delete("cursor");
    const queryString = params.toString();
    if (queryString !== searchParams.toString()) {
      router.replace(queryString ? pathname + "?" + queryString : pathname, { scroll: false });
    }
  }

  function goToPage(next: number) {
    const params = new URLSearchParams(searchParams.toString());
    params.delete("cursor");
    if (next <= 1) params.delete("page");
    else params.set("page", String(next));
    const queryString = params.toString();
    router.push(queryString ? pathname + "?" + queryString : pathname, { scroll: false });
  }

  return { searchParams, update, goToPage };
}
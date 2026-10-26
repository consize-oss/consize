import type { z } from "zod";
import { problemSchema } from "@/schemas";

const API_BASE = process.env.NEXT_PUBLIC_CONSIZE_API_BASE_URL ?? "http://127.0.0.1:8080";

export class ApiError extends Error {
  constructor(
    public status: number,
    public problem?: z.infer<typeof problemSchema>,
  ) {
    super(problem?.title ?? (status === 0 ? "Network unreachable" : `Request failed (${status})`));
  }
}

// Ensure development mocks are ready before retrying API requests.
// Reuse a running worker instead of restarting it for each retry.
export async function prepareApiRetry(): Promise<void> {
  if (
    typeof window !== "undefined"
    && process.env.NODE_ENV === "development"
    && process.env.NEXT_PUBLIC_USE_MOCKS === "true"
  ) {
    const { startMockServiceWorker } = await import("../mocks/browser");
    await startMockServiceWorker();
  }
}

// A lost development mock connection can outlive its successful startup promise.
// Only a manual Retry uses this fallback; real HTTP errors stay on the error card.
export function reloadForMockConnectionFailure(error: unknown): boolean {
  if (
    typeof window !== "undefined"
    && process.env.NODE_ENV === "development"
    && process.env.NEXT_PUBLIC_USE_MOCKS === "true"
    && error instanceof ApiError
    && error.status === 0
  ) {
    window.location.reload();
    return true;
  }
  return false;
}

export async function apiGet<T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal): Promise<T> {
  let res: Response;
  try {
    // Every browser request waits for mocks to be ready, including a manual Retry.
    // Keep this development-only and skip it during server rendering.
    if (
      typeof window !== "undefined"
      && process.env.NODE_ENV === "development"
      && process.env.NEXT_PUBLIC_USE_MOCKS === "true"
    ) {
      const { startMockServiceWorker } = await import("../mocks/browser");
      await startMockServiceWorker();
    }
    res = await fetch(`${API_BASE}${path}`, { signal, cache: "no-store" });
  } catch {
    // A network or mock-startup failure happened before receiving an HTTP response.
    throw new ApiError(0);
  }

  if (!res.ok) {
    const body = await res.json().catch(() => undefined);
    const parsed = problemSchema.safeParse(body);
    throw new ApiError(res.status, parsed.success ? parsed.data : undefined);
  }

  const json = await res.json();
  const parsed = schema.safeParse(json);
  if (!parsed.success) {
    // A 200 with the wrong shape is a contract break, not "unavailable" —
    // keep it distinguishable so schema drift never silently renders.
    throw new ApiError(-1, {
      title: "Response did not match the expected schema",
      status: -1,
      detail: parsed.error.message,
    });
  }
  return parsed.data;
}
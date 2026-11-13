import type { z } from "zod";
import { problemSchema } from "@/schemas";

const API_BASE = process.env.NEXT_PUBLIC_CONSIZE_API_BASE_URL ?? "http://127.0.0.1:8080";

export class ApiError extends Error {
  constructor(
    public status: number,
    public problem?: z.infer<typeof problemSchema>,
    public requestId?: string,
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

async function readApiResponse<T>(res: Response, schema: z.ZodType<T>): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => undefined);
    const parsed = problemSchema.safeParse(body);
    const backendError = typeof body?.error === "string" ? { title: body.error, status: res.status } : undefined;
    throw new ApiError(res.status, parsed.success ? parsed.data : backendError, res.headers.get("X-Request-ID") ?? undefined);
  }

  let json: unknown;
  try {
    json = await res.json();
  } catch {
    throw new ApiError(-1, { title: "Response was not valid JSON", status: -1 });
  }
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
function endpoint(path: string): string {
  try {
    const url = new URL(API_BASE);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error("Invalid API URL");
    return `${API_BASE.replace(/\/$/, "")}${path}`;
  } catch {
    throw new ApiError(-2, { title: "Invalid NEXT_PUBLIC_CONSIZE_API_BASE_URL: use an HTTP or HTTPS URL without credentials, query or fragment", status: -2 });
  }
}
async function request<T>(path: string, schema: z.ZodType<T>, signal: AbortSignal | undefined, method: "GET" | "POST", body?: unknown): Promise<T> {
  const url = endpoint(path);
  const timeout = AbortSignal.timeout(30_000);
  const combined = signal ? AbortSignal.any([signal, timeout]) : timeout;
  let res: Response;
  try {
    await prepareApiRetry();
    res = await fetch(url, { ...(method === "POST" ? { method } : {}), signal: combined, cache: "no-store", ...(body === undefined ? {} : { body: JSON.stringify(body), headers: { "Content-Type": "application/json" } }) });
  } catch {
    if (signal?.aborted) throw signal.reason;
    throw new ApiError(0, timeout.aborted ? { title: "API request timed out", status: 0 } : undefined);
  }
  return readApiResponse(res, schema);
}
export function apiGet<T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal): Promise<T> {
  return request(path, schema, signal, "GET");
}
export function apiPost<T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal, body?: unknown): Promise<T> {
  return request(path, schema, signal, "POST", body);
}

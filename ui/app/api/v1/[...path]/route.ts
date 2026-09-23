export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ path?: string[] }>;
};

const DEFAULT_API_UPSTREAM = "http://consize-api:8080";

async function proxy(request: Request, context: RouteContext): Promise<Response> {
  const params = await context.params;
  const path = (params.path ?? []).map(encodeURIComponent).join("/");
  const upstream = (process.env.API_UPSTREAM || DEFAULT_API_UPSTREAM).replace(/\/+$/, "");
  const requestURL = new URL(request.url);
  const targetURL = `${upstream}/api/v1/${path}${requestURL.search}`;

  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("content-length");

  const hasBody = request.method !== "GET" && request.method !== "HEAD";
  const response = await fetch(targetURL, {
    method: request.method,
    headers,
    body: hasBody ? await request.arrayBuffer() : undefined,
    cache: "no-store",
    redirect: "manual",
  });

  const responseHeaders = new Headers(response.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers: responseHeaders,
  });
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
export const HEAD = proxy;
export const OPTIONS = proxy;

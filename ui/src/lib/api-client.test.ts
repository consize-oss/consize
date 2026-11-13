import { afterEach, describe, it, mock } from "node:test";
import assert from "node:assert/strict";
import { z } from "zod";
import { apiGet, apiPost, ApiError } from "./api-client";
afterEach(() => mock.restoreAll());
const schema = z.object({ value: z.number() });
describe("API reads", () => {
  it("validates a response and forwards cancellation", async () => {
    const fetch = mock.method(globalThis, "fetch", (async () => new Response('{"value":0}')) as typeof globalThis.fetch);
    const controller = new AbortController();
    assert.deepEqual(await apiGet("/test", schema, controller.signal), { value: 0 });
    const args = fetch.mock.calls[0].arguments;
    assert.ok(String(args[0]).endsWith("/test"));
    assert.equal(args[1]?.cache, "no-store");
    controller.abort();
    assert.equal(args[1]?.signal?.aborted, true);
  });
  it("rejects malformed successful responses", async () => {
    mock.method(globalThis, "fetch", async () => new Response('{"value":"invalid"}'));
    await assert.rejects(apiGet("/test", schema), { status: -1 });
  });
  it("preserves permission refusal", async () => {
    mock.method(globalThis, "fetch", async () => new Response(
      JSON.stringify({ title: "Forbidden", status: 403 }), { status: 403 },
    ));
    await assert.rejects(apiGet("/test", schema), { status: 403, message: "Forbidden" });
  });
  it("reports network failure instead of inventing data", async () => {
    mock.method(globalThis, "fetch", async () => { throw new TypeError("connection refused"); });
    await assert.rejects(apiGet("/test", schema), (error: unknown) => error instanceof ApiError && error.status === 0);
  });
  it("preserves HTTP status for a non-JSON error", async () => {
    mock.method(globalThis, "fetch", async () => new Response("unavailable", { status: 503 }));
    await assert.rejects(apiGet("/test", schema), { status: 503 });
  });
});

describe("API commands", () => {
  it("sends a body-free POST, validates the response, and forwards cancellation", async () => {
    const fetch = mock.method(globalThis, "fetch", (async () => new Response('{"value":1}')) as typeof globalThis.fetch);
    const controller = new AbortController();
    assert.deepEqual(await apiPost("/api/discovery", schema, controller.signal), { value: 1 });
    assert.equal(fetch.mock.calls[0].arguments[1]?.method, "POST");
    controller.abort();
    assert.equal(fetch.mock.calls[0].arguments[1]?.signal?.aborted, true);
  });
  it("preserves permission refusal instead of reporting success", async () => {
    mock.method(globalThis, "fetch", async () => new Response(
      JSON.stringify({ title: "Forbidden", status: 403 }), { status: 403 },
    ));
    await assert.rejects(apiPost("/api/discovery", schema), { status: 403, message: "Forbidden" });
  });
  it("rejects successful responses with an invalid body", async () => {
    mock.method(globalThis, "fetch", async () => new Response('{"value":"wrong"}'));
    await assert.rejects(apiPost("/api/discovery", schema), { status: -1 });
    mock.restoreAll();
    mock.method(globalThis, "fetch", async () => new Response("not-json"));
    await assert.rejects(apiPost("/api/discovery", schema), { status: -1 });
  });
  it("reports network failures without inventing a successful sync", async () => {
    mock.method(globalThis, "fetch", async () => { throw new TypeError("unreachable"); });
    await assert.rejects(apiPost("/api/discovery", schema), { status: 0 });
  });
});

it("preserves the documented backend error and request correlation", async () => {
 mock.method(globalThis,"fetch",async()=>new Response(JSON.stringify({error:"Live preflight blocked"}),{status:400,headers:{"X-Request-ID":"request-123"}}));
 await assert.rejects(apiPost("/execute",schema),{message:"Live preflight blocked",requestId:"request-123"});
});
it("serializes explicit approval and never retries a command", async () => {
 const fetch=mock.method(globalThis,"fetch",async()=>new Response('{"value":1}'));
 await apiPost("/execute",schema,undefined,{mode:"approved"});
 assert.equal(fetch.mock.callCount(),1);
 assert.equal(fetch.mock.calls[0].arguments[1]?.body,'{"mode":"approved"}');
});

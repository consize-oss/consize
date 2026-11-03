import { afterEach, it, mock } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { apiGet, ApiError } from "./api-client";
import { resourceListSchema } from "../features/resources/queries";
import { buildResourceInventory } from "../features/resources/resourceInventoryModel";

afterEach(() => mock.restoreAll());

it("renders zero resources from an empty backend response without invented assessments", async () => {
  mock.method(globalThis, "fetch", async () => new Response('{"resources":[]}'));
  const response = await apiGet("/api/resources", resourceListSchema);
  const result = buildResourceInventory(response.resources, {}, {
    q: "", environment: "", owner: "", coverage: "", telemetry: "", optimization: "",
  }, "all", 1);
  assert.equal(result.summary.totalResources, 0);
  assert.deepEqual(result.counts, { all: 0, attention: 0, unmonitored: 0 });
  assert.deepEqual(result.pagination.items, []);
  assert.equal(result.summary.automationPercentage, null);
});

it("keeps unavailable backend state as an error instead of substituting demo resources", async () => {
  mock.method(globalThis, "fetch", async () => { throw new TypeError("unreachable"); });
  await assert.rejects(apiGet("/api/resources", resourceListSchema), (error: unknown) =>
    error instanceof ApiError && error.status === 0);
});

it("normal app static imports cannot load development fixtures", () => {
  const root = new URL("../", import.meta.url);
  const visited = new Set<string>();
  function visit(url: URL) {
    if (visited.has(url.href)) return;
    visited.add(url.href);
    assert.ok(!url.pathname.includes("/mocks/") && !url.pathname.includes("/components/dev/"), `fixture imported by normal app: ${url.pathname}`);
    const ast = ts.createSourceFile(url.pathname, readFileSync(url, "utf8"), ts.ScriptTarget.Latest);
    for (const statement of ast.statements) {
      if (!ts.isImportDeclaration(statement) && !ts.isExportDeclaration(statement)) continue;
      const specifier = statement.moduleSpecifier;
      if (!specifier || !ts.isStringLiteral(specifier)) continue;
      const path = specifier.text;
      const base = path.startsWith("@/") ? new URL(path.slice(2), root)
        : path.startsWith(".") ? new URL(path, url) : null;
      if (!base) continue;
      for (const suffix of [".ts", ".tsx", "/index.ts", "/index.tsx"]) {
        const candidate = new URL(base.href + suffix);
        if (existsSync(fileURLToPath(candidate))) { visit(candidate); break; }
      }
    }
  }
  visit(new URL("app/layout.tsx", root));
  visit(new URL("app/page.tsx", root));
  visit(new URL("app/resources/page.tsx", root));
  visit(new URL("app/plugins/page.tsx", root));
});

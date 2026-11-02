// Prove the exact compiler and test runner fail on synthetic defects.
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
const root = fileURLToPath(new URL("../../", import.meta.url));
const temporary = mkdtempSync(join(tmpdir(), "consize-ci-controls-"));
function rejected(label, args, expected) {
  const result = spawnSync(process.execPath, args, { cwd: root, encoding: "utf8", timeout: 60_000 });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}`;
  if (result.error || result.status !== 1 && result.status !== 2 || !output.includes(expected)) {
    throw new Error(`${label} did not fail for the intended reason: ${output}`);
  }
  console.log(`PASS: ${label} rejected the synthetic defect`);
}
try {
  const badType = join(temporary, "bad-type.ts");
  writeFileSync(badType, 'const amount: number = "invalid";\n');
  rejected("TypeScript", [join(root, "ui/node_modules/typescript/bin/tsc"), "--noEmit", "--skipLibCheck", badType], "TS2322");
  const badTest = join(temporary, "bad.test.ts");
  writeFileSync(badTest, 'import { test } from "node:test"; import assert from "node:assert/strict"; test("synthetic rejection", () => assert.equal(1, 2));\n');
  rejected("frontend test runner", ["--import", join(root, "ui/tests/typescript-loader.mjs"), "--test", badTest], "ERR_ASSERTION");
} finally {
  rmSync(temporary, { recursive: true, force: true });
}

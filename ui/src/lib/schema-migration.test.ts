import { test } from "node:test";
import assert from "node:assert/strict";
import { baseRecommendations, buildSummary, detailFor } from "../mocks/fixtures";
import { dashboardSummarySchema, recommendationDetailSchema, recommendationListItemSchema } from "../schemas";

test("patched schema library preserves supported recommendation payloads", () => {
  for (const recommendation of baseRecommendations) {
    assert.equal(recommendationListItemSchema.safeParse(recommendation).success, true, recommendation.id);
    const detail = detailFor(recommendation.id);
    if (detail) assert.equal(recommendationDetailSchema.safeParse(detail).success, true, recommendation.id);
  }
});
test("dictionary values still reject invalid API data", () => {
  const valid = structuredClone(baseRecommendations[0]);
  valid.resource.tags = { team: "platform" };
  assert.equal(recommendationListItemSchema.safeParse(valid).success, true);
  assert.equal(recommendationListItemSchema.safeParse({ ...valid, resource: { ...valid.resource, tags: { team: 123 } } }).success, false);
});
test("empty and degraded dashboard payloads keep their schema guarantees", () => {
  for (const empty of [true, false]) {
    for (const degraded of [true, false]) {
      const summary = buildSummary({}, "P30D", { stale: false, degraded, empty });
      assert.equal(dashboardSummarySchema.safeParse(summary).success, true);
    }
  }
});

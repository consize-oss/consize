import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { getClientPage } from "./pagination";

const resources = Array.from({ length: 50 }, (_, index) => ({ id: index + 1 }));

describe("Complete-list pagination", () => {
  it("uses seven rows per page and reports accurate record ranges", () => {
    const first = getClientPage(resources, 1, 7);
    const second = getClientPage(resources, 2, 7);
    assert.deepEqual(first.items.map((item) => item.id), [1, 2, 3, 4, 5, 6, 7]);
    assert.deepEqual(second.items.map((item) => item.id), [8, 9, 10, 11, 12, 13, 14]);
    assert.equal(first.totalPages, 8);
    assert.equal(first.totalItems, 50);
    assert.deepEqual([first.startItem, first.endItem], [1, 7]);
    assert.deepEqual([second.startItem, second.endItem], [8, 14]);
  });

  it("keeps the final partial page and never duplicates or loses records", () => {
    const last = getClientPage(resources, 8, 7);
    assert.deepEqual(last.items, [{ id: 50 }]);
    assert.deepEqual([last.startItem, last.endItem], [50, 50]);
    const visited = Array.from({ length: 8 }, (_, index) => getClientPage(resources, index + 1, 7).items).flat();
    assert.deepEqual(visited, resources);
  });

  it("handles an empty result without inventing record one", () => {
    const empty = getClientPage([], 8, 7);
    assert.deepEqual(empty.items, []);
    assert.deepEqual([empty.currentPage, empty.totalPages, empty.totalItems, empty.startItem, empty.endItem], [1, 1, 0, 0, 0]);
  });

  it("uses page one for malformed page numbers and clamps pages after results shrink", () => {
    for (const page of [NaN, Infinity, -1, 0, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
      assert.equal(getClientPage(resources, page, 7).currentPage, 1);
    }
    assert.equal(getClientPage(resources, 999, 7).currentPage, 8);
    const narrowed = getClientPage(resources.slice(0, 3), 8, 7);
    assert.equal(narrowed.currentPage, 1);
    assert.deepEqual(narrowed.items, resources.slice(0, 3));
  });

  it("accepts different page sizes without mutating the input", () => {
    const before = [...resources];
    assert.equal(getClientPage(resources, 2, 5).startItem, 6);
    assert.equal(getClientPage(resources, 2, 5).endItem, 10);
    assert.deepEqual(resources, before);
    for (const pageSize of [0, -1, 1.5, Infinity]) {
      assert.throws(() => getClientPage(resources, 1, pageSize), RangeError);
    }
  });
});

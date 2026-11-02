import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
const source = readFileSync(new URL("../../docs/javascripts/copy-page.js", import.meta.url), "utf8");

for (const [pathname, expected] of [["/guide/index.html", "/guide/index.md"], ["/guide/", "/guide/index.md"], ["/guide", "/guide/index.md"], ["/index.htmlindex.html", "/index.htmlindex.html/index.md"]]) {
  test(`copy page URL for ${pathname}`, async () => {
    let mounted;
    let fetched;
    const node = () => ({ children: [], setAttribute() {}, append(...children) { this.children.push(...children); }, addEventListener(type, fn) { this[type] = fn; }, replaceWith() {} });
    const toc = { append(value) { mounted = value; } };
    const context = {
      location: { pathname, origin: "https://docs.example.test" },
      document: {
        readyState: "complete", createElement: node, createElementNS: node,
        querySelectorAll: () => [], querySelector: selector => selector.includes("sidebar") ? toc : {},
      },
      navigator: { clipboard: { async writeText(text) { assert.equal(text, "synthetic markdown"); } } },
      window: {}, setTimeout() {},
      async fetch(url) { fetched = url; return { ok: true, headers: { get: () => "text/plain" }, text: async () => "synthetic markdown" }; },
    };
    vm.runInNewContext(source, context);
    const button = mounted.children[0];
    button.firstChild = button.children[0];
    await button.click();
    assert.equal(fetched, `https://docs.example.test${expected}`);
  });
}

test("copied Markdown table escapes both backslashes and pipes", async () => {
  let mounted;
  let copied;
  const node = () => ({ children: [], setAttribute() {}, append(...children) { this.children.push(...children); }, addEventListener(type, fn) { this[type] = fn; }, replaceWith() {} });
  const cell = { nodeType: 3, textContent: String.raw`a\|b` };
  const root = {
    nodeType: 1, tagName: "TABLE", className: "", cloneNode() { return this; },
    querySelectorAll: selector => selector === "tr" ? [{ children: [cell] }] : [],
  };
  vm.runInNewContext(source, {
    location: { pathname: "/guide/", origin: "https://docs.example.test" },
    document: {
      readyState: "complete", createElement: node, createElementNS: node, querySelectorAll: () => [],
      querySelector: selector => selector.includes("sidebar") ? { append(value) { mounted = value; } } : root,
    },
    navigator: { clipboard: { async writeText(text) { copied = text; } } },
    window: {}, setTimeout() {}, async fetch() { return { ok: false }; },
  });
  const button = mounted.children[0];
  button.firstChild = button.children[0];
  await button.click();
  assert.ok(copied.includes(String.raw`a\\\|b`), copied);
});

test("search suggestions use safe DOM nodes and preserve nested documentation links", () => {
  for (const [base, expectedPrefix] of [["../..", "https://docs.example.test/consize/"], ['\"><img src=x onerror=alert(1)>', "https://docs.example.test/"], ["javascript:alert(1)//", "https://docs.example.test/consize/guides/nested/"], ["//evil.example.test", "https://docs.example.test/consize/guides/nested/"]]) {
    let panel;
    const input = { value: "", setAttribute() {}, addEventListener(type, fn) { this[type] = fn; } };
    const result = { style: {} };
    const makeNode = tag => ({
      tag, children: [], attributes: {}, style: {}, textContent: "",
      set innerHTML(_value) { throw new Error("HTML parsing must not be used for search suggestions"); },
      setAttribute(key, value) { this.attributes[key] = value; },
      appendChild(child) { this.children.push(child); return child; },
    });
    const document = {
      body: {},
      querySelector(selector) {
        if (selector === ".md-search__input") return input;
        if (selector === ".md-search__scrollwrap") return { insertBefore(value) { panel = value; } };
        if (selector === ".md-search-result") return result;
        return null;
      },
      getElementById() { return { textContent: JSON.stringify({ base }) }; },
      createElement: makeNode, createElementNS: (_namespace, tag) => makeNode(tag),
      addEventListener(type, callback) { if (type === "DOMContentLoaded") callback(); },
    };
    const source = readFileSync(new URL("../../docs/javascripts/search-suggestions.js", import.meta.url), "utf8");
    const location = new URL("https://docs.example.test/consize/guides/nested/");
    vm.runInNewContext(source, { document, location, URL, MutationObserver: class { observe() {} } });
    assert.equal(panel.children[0].textContent, "Suggested");
    const links = panel.children[1].children.map(item => item.children[0]);
    assert.equal(links.length, 6);
    for (const link of links) {
      const url = new URL(link.href);
      assert.equal(url.origin, location.origin);
      assert.equal(url.protocol, "https:");
      assert.ok(link.href.startsWith(expectedPrefix), link.href);
      assert.equal(link.children[0].children[0].tag, "svg");
    }
    assert.equal(links[0].children[1].children[0].textContent, "Get started");
    assert.equal(panel.style.display, "");
    input.value = "query"; input.input();
    assert.equal(panel.style.display, "none");
    assert.equal(result.style.display, "");
  }
});

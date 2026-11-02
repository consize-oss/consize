/* Consize docs: "Copy page" button under the table of contents (Material for MkDocs).
   Styling lives in extra.css (section "Copy page"). */
(() => {
  "use strict";

  const ICONS = {
    copy: "M19 21H8V7h11m0-2H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2m-3-4H4a2 2 0 0 0-2 2v14h2V3h12z",
    check: "M21 7 9 19l-5.5-5.5 1.41-1.41L9 16.17 19.59 5.59z",
  };
  const icon = (n) => {
    const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    s.setAttribute("viewBox", "0 0 24 24");
    s.setAttribute("width", "14"); // sensible size even before the stylesheet applies
    s.setAttribute("height", "14");
    s.setAttribute("aria-hidden", "true");
    s.setAttribute("class", "consize-copy-page__icon");
    const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
    p.setAttribute("d", ICONS[n]);
    p.setAttribute("fill", "currentColor");
    s.append(p);
    return s;
  };
  const h = (tag, attrs = {}, ...kids) => {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
    e.append(...kids);
    return e;
  };

  /* ---------- clipboard ---------- */
  async function copyText(text) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const t = h("textarea", { style: "position:fixed;opacity:0" });
      t.value = text;
      document.body.append(t);
      t.select();
      const ok = document.execCommand("copy");
      t.remove();
      if (!ok) throw new Error("copy failed");
    }
  }

  /* ---------- page -> Markdown (for "Copy page") ---------- */
  function md(n, pre = false) {
    if (n.nodeType === 3) return pre ? n.textContent : n.textContent.replace(/\s+/g, " ");
    if (n.nodeType !== 1) return "";
    const tag = n.tagName.toLowerCase();
    const kids = (p = pre) => [...n.childNodes].map((c) => md(c, p)).join("");
    const cls = n.className && n.className.baseVal === undefined ? n.className : "";
    if (["svg", "button", "input", "nav", "script", "style"].includes(tag)) return "";
    if (/^h[1-6]$/.test(tag)) return `\n\n${"#".repeat(+tag[1])} ${kids().trim()}\n\n`;
    switch (tag) {
      case "p": return `\n\n${kids().trim()}\n\n`;
      case "br": return "  \n";
      case "hr": return "\n\n---\n\n";
      case "strong": case "b": return `**${kids().trim()}**`;
      case "em": case "i": return `*${kids().trim()}*`;
      case "img": return `![${n.alt || ""}](${n.src})`;
      case "code": return pre ? kids() : `\`${n.textContent}\``;
      case "a": { const t = kids().trim(); return t && n.href ? `[${t}](${n.href})` : t; }
      case "pre": {
        const code = n.querySelector("code") || n;
        const m = (n.closest("[class*=language-]")?.className || code.className || "").match(/language-([\w+-]+)/);
        return `\n\n\`\`\`${m ? m[1] : ""}\n${code.textContent.replace(/\n$/, "")}\n\`\`\`\n\n`;
      }
      case "ul": case "ol": {
        let k = 0;
        const items = [...n.children].filter((c) => c.tagName === "LI").map((li) => {
          const b = tag === "ol" ? `${++k}. ` : "- ";
          return b + md(li).trim().replace(/\n\n+/g, "\n").replace(/\n/g, "\n" + " ".repeat(b.length));
        });
        return `\n\n${items.join("\n")}\n\n`;
      }
      case "table": {
        const rows = [...n.querySelectorAll("tr")].map((tr) => [...tr.children].map((c) => md(c).trim().replace(/[\\|]/g, "\\$&").replace(/\n+/g, " ")));
        if (!rows.length) return "";
        return "\n\n" + [rows[0], rows[0].map(() => "---"), ...rows.slice(1)].map((r) => `| ${r.join(" | ")} |`).join("\n") + "\n\n";
      }
      case "blockquote": return "\n\n" + kids().trim().split("\n").map((l) => (l ? "> " + l : ">")).join("\n") + "\n\n";
      case "details": case "div": case "section": break;
      default: return kids();
    }
    if (/\badmonition\b/.test(cls) || tag === "details") {
      const title = n.querySelector(":scope > .admonition-title, :scope > summary");
      const body = [...n.childNodes].filter((c) => c !== title).map((c) => md(c)).join("").trim();
      return "\n\n" + `**${title ? title.textContent.trim() : "Note"}**\n\n${body}`.split("\n").map((l) => (l ? "> " + l : ">")).join("\n") + "\n\n";
    }
    if (/\btabbed-set\b/.test(cls)) {
      const labels = [...n.querySelectorAll(":scope > .tabbed-labels > label")];
      return [...n.querySelectorAll(":scope > .tabbed-content > .tabbed-block")]
        .map((b, j) => `\n\n**${labels[j] ? labels[j].textContent.trim() : ""}**\n\n${md(b)}`).join("");
    }
    return kids();
  }
  function domToMarkdown(root) {
    const c = root.cloneNode(true);
    c.querySelectorAll(".consize-copy-page, .headerlink, .md-content__button, .md-source-file, .md-clipboard, .md-code__nav, [class*=feedback]").forEach((x) => x.remove());
    return md(c).replace(/^[ \t]+$/gm, "").replace(/\n{3,}/g, "\n\n").trim() + "\n";
  }

  // The mkdocs-llmstxt plugin publishes a clean index.md next to many pages; fall back to the rendered page.
  async function pageMarkdown() {
    const segments = location.pathname.split("/");
    if (segments[segments.length - 1] === "index.html") segments.pop();
    const path = segments.join("/");
    const dir = path.endsWith("/") ? path : path + "/";
    try {
      const r = await fetch(location.origin + dir + "index.md");
      if (r.ok && !(r.headers.get("content-type") || "").includes("text/html")) return await r.text();
    } catch { /* use fallback */ }
    return domToMarkdown(document.querySelector(".md-content__inner"));
  }

  function copyButton() {
    const label = h("span", {}, "Copy page");
    const btn = h("button", { type: "button", class: "consize-copy-page__btn" }, icon("copy"), label);
    btn.addEventListener("click", async () => {
      try { await copyText(await pageMarkdown()); label.textContent = "Copied"; btn.firstChild.replaceWith(icon("check")); }
      catch { label.textContent = "Copy failed"; }
      setTimeout(() => { label.textContent = "Copy page"; btn.firstChild.replaceWith(icon("copy")); }, 2000);
    });
    return h("div", { class: "consize-copy-page", "aria-live": "polite" }, btn);
  }
  // Lives under the table of contents only (right sidebar, shown on wide screens).
  function mountCopyButtons() {
    document.querySelectorAll(".consize-copy-page").forEach((n) => n.remove());
    const toc = document.querySelector(".md-sidebar--secondary:not([hidden]) .md-sidebar__inner");
    if (toc && document.querySelector(".md-content__inner")) toc.append(copyButton());
  }

  // Material's instant navigation swaps page content without reloading, so re-mount on every page.
  if (window.document$) window.document$.subscribe(mountCopyButtons);
  else if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mountCopyButtons);
  else mountCopyButtons();
})();
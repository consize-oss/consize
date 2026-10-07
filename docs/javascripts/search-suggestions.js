function nameSearchDialog() {
  var dialog = document.querySelector('.md-search[role="dialog"]');
  if (dialog) {
    dialog.setAttribute("aria-label", "Search documentation");
  }
}

nameSearchDialog();

var observer = new MutationObserver(nameSearchDialog);
observer.observe(document.body, {
  childList: true,
  subtree: true
});

document.addEventListener("DOMContentLoaded", function () {
  // Icon paths (24x24 viewBox), same set used in the top-nav dropdown.
  var icons = {
    "rocket-launch": "m13.13 22.19-1.63-3.83c1.57-.58 3.04-1.36 4.4-2.27zM5.64 12.5l-3.83-1.63 6.1-2.77C7 9.46 6.22 10.93 5.64 12.5M21.61 2.39S16.66.269 11 5.93c-2.19 2.19-3.5 4.6-4.35 6.71-.28.75-.09 1.57.46 2.13l2.13 2.12c.55.56 1.37.74 2.12.46A19.1 19.1 0 0 0 18.07 13c5.66-5.66 3.54-10.61 3.54-10.61m-7.07 7.07c-.78-.78-.78-2.05 0-2.83s2.05-.78 2.83 0c.77.78.78 2.05 0 2.83s-2.05.78-2.83 0m-5.66 7.07-1.41-1.41zM6.24 22l3.64-3.64c-.34-.09-.67-.24-.97-.45L4.83 22zM2 22h1.41l4.77-4.76-1.42-1.41L2 20.59zm0-2.83 4.09-4.08c-.21-.3-.36-.62-.45-.97L2 17.76z",
    "flask-outline": "M5 19a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1c0-.21-.07-.41-.18-.57L13 8.35V4h-2v4.35L5.18 18.43c-.11.16-.18.36-.18.57m1 3a3 3 0 0 1-3-3c0-.6.18-1.16.5-1.63L9 7.81V6a1 1 0 0 1-1-1V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v1a1 1 0 0 1-1 1v1.81l5.5 9.56c.32.47.5 1.03.5 1.63a3 3 0 0 1-3 3zm7-6 1.34-1.34L16.27 18H7.73l2.66-4.61zm-.5-4a.5.5 0 0 1 .5.5.5.5 0 0 1-.5.5.5.5 0 0 1-.5-.5.5.5 0 0 1 .5-.5",
    "server-outline": "M2 4.6v4.8c0 .9.5 1.6 1.2 1.6h17.7c.6 0 1.2-.7 1.2-1.6V4.6C22 3.7 21.5 3 20.8 3H3.2C2.5 3 2 3.7 2 4.6M10 8V6H9v2zM5 8h2V6H5zm15 1H4V5h16zM2 14.6v4.8c0 .9.5 1.6 1.2 1.6h17.7c.6 0 1.2-.7 1.2-1.6v-4.8c0-.9-.5-1.6-1.2-1.6H3.2c-.7 0-1.2.7-1.2 1.6m8 3.4v-2H9v2zm-5 0h2v-2H5zm15 1H4v-4h16z",
    "arrow-collapse-vertical": "M4 12h16v2H4zm0-3h16v2H4zm12-5-4 4-4-4h3V1h2v3zM8 19l4-4 4 4h-3v3h-2v-3z",
    "cog-outline": "M12 8a4 4 0 0 1 4 4 4 4 0 0 1-4 4 4 4 0 0 1-4-4 4 4 0 0 1 4-4m0 2a2 2 0 0 0-2 2 2 2 0 0 0 2 2 2 2 0 0 0 2-2 2 2 0 0 0-2-2m-2 12c-.25 0-.46-.18-.5-.42l-.37-2.65c-.63-.25-1.17-.59-1.69-.99l-2.49 1.01c-.22.08-.49 0-.61-.22l-2-3.46a.493.493 0 0 1 .12-.64l2.11-1.66L4.5 12l.07-1-2.11-1.63a.493.493 0 0 1-.12-.64l2-3.46c.12-.22.39-.31.61-.22l2.49 1c.52-.39 1.06-.73 1.69-.98l.37-2.65c.04-.24.25-.42.5-.42h4c.25 0 .46.18.5.42l.37 2.65c.63.25 1.17.59 1.69.98l2.49-1c.22-.09.49 0 .61.22l2 3.46c.13.22.07.49-.12.64L19.43 11l.07 1-.07 1 2.11 1.63c.19.15.25.42.12.64l-2 3.46c-.12.22-.39.31-.61.22l-2.49-1c-.52.39-1.06.73-1.69.98l-.37 2.65c-.04.24-.25.42-.5.42zm1.25-18-.37 2.61c-1.2.25-2.26.89-3.03 1.78L5.44 7.35l-.75 1.3L6.8 10.2a5.55 5.55 0 0 0 0 3.6l-2.12 1.56.75 1.3 2.43-1.04c.77.88 1.82 1.52 3.01 1.76l.37 2.62h1.52l.37-2.61c1.19-.25 2.24-.89 3.01-1.77l2.43 1.04.75-1.3-2.12-1.55c.4-1.17.4-2.44 0-3.61l2.11-1.55-.75-1.3-2.41 1.04a5.42 5.42 0 0 0-3.03-1.77L12.75 4z",
    "tune-variant": "M8 13c-1.86 0-3.41 1.28-3.86 3H2v2h2.14c.45 1.72 2 3 3.86 3s3.41-1.28 3.86-3H22v-2H11.86c-.45-1.72-2-3-3.86-3m0 6c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2M19.86 6c-.45-1.72-2-3-3.86-3s-3.41 1.28-3.86 3H2v2h10.14c.45 1.72 2 3 3.86 3s3.41-1.28 3.86-3H22V6zM16 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2"
  };

  // Curated links shown before the user types anything —
  // mirrors Merge's docs search: click in, see useful destinations immediately.
  var suggestions = [
    { icon: "rocket-launch", title: "Get started", desc: "Go from zero to a verified rightsizing change.", url: "getting-started/get-started/" },
    { icon: "flask-outline", title: "Interactive Sandbox", desc: "Try Consize locally in one Docker container.", url: "getting-started/sandbox/" },
    { icon: "server-outline", title: "Production Installation", desc: "Install the Helm chart onto a live cluster.", url: "getting-started/installation/" },
    { icon: "arrow-collapse-vertical", title: "Kubernetes Rightsizing", desc: "How CPU/memory requests and limits get resized.", url: "guides/rightsizing/" },
    { icon: "cog-outline", title: "How Consize Works", desc: "The full Observe → Analyze → Verify architecture.", url: "concepts/architecture/" },
    { icon: "tune-variant", title: "Configuration", desc: "Full reference for what Consize can observe and change.", url: "reference/configuration/" }
  ];

  var input = document.querySelector(".md-search__input");
  var scrollwrap = document.querySelector(".md-search__scrollwrap");
  var result = document.querySelector(".md-search-result");
  if (!input || !scrollwrap || !result) return;

  // Material exposes the page's relative base path in #__config so links
  // resolve correctly no matter how deep the current page is nested.
  var base = ".";
  try {
    base = JSON.parse(document.getElementById("__config").textContent).base;
  } catch (e) {}

  var panel = document.createElement("div");
  panel.className = "consize-search-suggestions";
  panel.innerHTML =
    '<div class="consize-search-suggestions__label">Suggested</div>' +
    '<ul class="consize-search-suggestions__list">' +
    suggestions.map(function (s) {
      return (
        '<li><a class="consize-search-suggestions__link" href="' + base + "/" + s.url + '">' +
          '<span class="consize-search-suggestions__icon" aria-hidden="true">' +
            '<svg viewBox="0 0 24 24"><path d="' + icons[s.icon] + '"/></svg>' +
          '</span>' +
          '<span class="consize-search-suggestions__text">' +
            '<span class="consize-search-suggestions__title">' + s.title + '</span>' +
            '<span class="consize-search-suggestions__desc">' + s.desc + '</span>' +
          '</span>' +
        '</a></li>'
      );
    }).join("") +
    '</ul>';

  scrollwrap.insertBefore(panel, result);

  function sync() {
    var empty = input.value.trim().length === 0;
    panel.style.display = empty ? "" : "none";
    result.style.display = empty ? "none" : "";
  }

  input.addEventListener("focus", sync);
  input.addEventListener("input", sync);
  sync();
});
document.querySelector(".md-search__input")?.setAttribute("placeholder", "Search docs");
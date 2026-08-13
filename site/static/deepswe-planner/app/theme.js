"use strict";

const applyTheme = (theme) => {
  if (theme !== "light" && theme !== "dark") return;
  document.documentElement.dataset.theme = theme;
};

applyTheme(new URLSearchParams(location.search).get("theme"));

window.addEventListener("message", (event) => {
  if (event.origin !== location.origin || event.source !== window.parent) return;
  if (event.data?.type !== "agent-layer-theme") return;
  applyTheme(event.data.theme);
});

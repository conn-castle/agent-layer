"use strict";

const requestedTheme = new URLSearchParams(location.search).get("theme");
if (requestedTheme === "light" || requestedTheme === "dark") {
  document.documentElement.dataset.theme = requestedTheme;
}

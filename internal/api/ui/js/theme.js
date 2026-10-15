const root = document.documentElement;

try {
  if (localStorage.getItem("theme") === "light") root.dataset.theme = "light";
} catch {
  // Storage is unavailable in some private windows; stay on the default.
}

document.addEventListener("click", (event) => {
  if (!event.target.closest("[data-theme-toggle]")) return;
  const light = root.dataset.theme !== "light";
  if (light) root.dataset.theme = "light";
  else delete root.dataset.theme;
  try {
    localStorage.setItem("theme", light ? "light" : "dark");
  } catch {}
});

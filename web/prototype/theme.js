// Theme for the static prototype: ?theme=light|dark, then the saved choice, then the system setting.
(function () {
  var root = document.documentElement;
  var fromQuery = new URLSearchParams(location.search).get("theme");
  var saved = null;
  try { saved = localStorage.getItem("assess-theme"); } catch (e) { /* storage may be blocked */ }
  var chosen = fromQuery || saved;
  if (chosen === "light" || chosen === "dark") root.dataset.theme = chosen;

  window.toggleTheme = function () {
    var current = root.dataset.theme ||
      (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    var next = current === "dark" ? "light" : "dark";
    root.dataset.theme = next;
    try { localStorage.setItem("assess-theme", next); } catch (e) { /* ignore */ }
  };
})();

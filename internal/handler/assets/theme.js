(function () {
  var KEY = "theme";

  function stored() {
    try {
      var t = localStorage.getItem(KEY);
      if (t === "light" || t === "dark") return t;
    } catch (e) {}
    return null;
  }

  function apply(t) {
    if (t === "light" || t === "dark") {
      document.documentElement.setAttribute("data-theme", t);
    } else {
      document.documentElement.removeAttribute("data-theme");
    }
  }

  apply(stored());

  function current() {
    var t = stored();
    if (t) return t;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  function bind() {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    function label() {
      var next = current() === "dark" ? "Light" : "Dark";
      btn.textContent = next;
      btn.setAttribute("aria-label", "Switch to " + next.toLowerCase() + " theme");
    }
    label();
    btn.addEventListener("click", function () {
      var next = current() === "dark" ? "light" : "dark";
      try {
        localStorage.setItem(KEY, next);
      } catch (e) {}
      apply(next);
      label();
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bind);
  } else {
    bind();
  }
})();

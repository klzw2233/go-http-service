(function () {
  var TOKEN_KEY = "authorTokens";

  function tokens() {
    try {
      var raw = sessionStorage.getItem(TOKEN_KEY);
      if (!raw) return null;
      var t = JSON.parse(raw);
      if (!t || !t.access_token) return null;
      return t;
    } catch (e) {
      return null;
    }
  }

  function saveTokens(pair) {
    sessionStorage.setItem(TOKEN_KEY, JSON.stringify({
      access_token: pair.access_token,
      refresh_token: pair.refresh_token,
      expires_at: pair.expires_at
    }));
  }

  function clearTokens() {
    sessionStorage.removeItem(TOKEN_KEY);
  }

  function showError(msg) {
    var el = document.getElementById("error");
    if (!el) return;
    el.hidden = !msg;
    el.textContent = msg || "";
  }

  function loginURL() {
    var next = location.pathname + location.search;
    return "/author/login?next=" + encodeURIComponent(next);
  }

  function requireToken() {
    if (tokens()) return true;
    location.replace(loginURL());
    return false;
  }

  function api(path, opts) {
    opts = opts || {};
    var headers = opts.headers || {};
    var t = tokens();
    if (t && t.access_token) {
      headers["Authorization"] = "Bearer " + t.access_token;
    }
    if (opts.body && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
    return fetch(path, {
      method: opts.method || "GET",
      headers: headers,
      body: opts.body || undefined
    }).then(function (res) {
      if (res.status !== 401 || opts._retried) return res;
      return refresh().then(function (ok) {
        if (!ok) {
          location.replace(loginURL());
          return res;
        }
        opts._retried = true;
        return api(path, opts);
      });
    });
  }

  function refresh() {
    var t = tokens();
    if (!t || !t.refresh_token) return Promise.resolve(false);
    return fetch("/api/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: t.refresh_token })
    }).then(function (res) {
      if (!res.ok) {
        clearTokens();
        return false;
      }
      return res.json().then(function (pair) {
        saveTokens(pair);
        return true;
      });
    }).catch(function () {
      return false;
    });
  }

  function bindLogout() {
    var btn = document.getElementById("logout");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var t = tokens();
      var body = t && t.refresh_token
        ? JSON.stringify({ refresh_token: t.refresh_token })
        : "{}";
      fetch("/api/auth/logout", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: body
      }).finally(function () {
        clearTokens();
        location.replace("/author/login");
      });
    });
  }

  function afterLoginPath() {
    var params = new URLSearchParams(location.search);
    var next = params.get("next") || "/author/posts";
    if (next.indexOf("/author/") !== 0) next = "/author/posts";
    return next;
  }

  function initLogin() {
    var form = document.getElementById("login-form");
    if (!form) return;
    if (tokens()) {
      location.replace(afterLoginPath());
      return;
    }
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      showError("");
      var data = new FormData(form);
      fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          username: data.get("username"),
          password: data.get("password")
        })
      }).then(function (res) {
        if (!res.ok) {
          showError("Sign in failed.");
          return;
        }
        return res.json().then(function (pair) {
          if (!pair || !pair.access_token) {
            showError("Sign in failed.");
            return;
          }
          saveTokens(pair);
          location.replace(afterLoginPath());
        });
      }).catch(function () {
        showError("Sign in failed.");
      });
    });
  }

  function initList() {
    if (!document.getElementById("rows")) return;
    if (!requireToken()) return;
    bindLogout();
    api("/api/posts").then(function (res) {
      if (!res.ok) {
        showError("Could not load posts.");
        return;
      }
      return res.json().then(function (posts) {
        var tbody = document.getElementById("rows");
        tbody.textContent = "";
        (posts || []).forEach(function (p) {
          var tr = document.createElement("tr");
          var title = document.createElement("td");
          var a = document.createElement("a");
          a.href = "/author/posts/" + encodeURIComponent(p.slug);
          a.textContent = p.title;
          title.appendChild(a);
          var slug = document.createElement("td");
          slug.textContent = p.slug;
          var state = document.createElement("td");
          state.textContent = p.draft ? "Draft" : "Published";
          tr.appendChild(title);
          tr.appendChild(slug);
          tr.appendChild(state);
          tbody.appendChild(tr);
        });
      });
    }).catch(function () {
      showError("Could not load posts.");
    });
  }

  function initEditor() {
    var form = document.getElementById("edit-form");
    if (!form) return;
    if (!requireToken()) return;
    bindLogout();
    var mode = document.body.getAttribute("data-mode");
    var titleEl = document.getElementById("title");
    var slugEl = document.getElementById("slug");
    var bodyEl = document.getElementById("body");
    var previewEl = document.getElementById("preview");

    if (mode === "edit") {
      var parts = location.pathname.split("/");
      var slug = decodeURIComponent(parts[parts.length - 1]);
      api("/api/posts/" + encodeURIComponent(slug)).then(function (res) {
        if (!res.ok) {
          showError("Could not load post.");
          return;
        }
        return res.json().then(function (p) {
          titleEl.value = p.title;
          slugEl.value = p.slug;
          bodyEl.value = p.body;
        });
      });
    }

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      showError("");
      var payload = {
        title: titleEl.value,
        slug: slugEl.value,
        body: bodyEl.value
      };
      var req = mode === "new"
        ? api("/api/posts", { method: "POST", body: JSON.stringify(payload) })
        : api("/api/posts/" + encodeURIComponent(slugEl.value), {
            method: "PATCH",
            body: JSON.stringify({ title: payload.title, body: payload.body })
          });
      req.then(function (res) {
        if (!res.ok) {
          showError("Save failed.");
          return;
        }
        return res.json().then(function (p) {
          location.replace("/author/posts/" + encodeURIComponent(p.slug));
        });
      }).catch(function () {
        showError("Save failed.");
      });
    });

    document.getElementById("preview-btn").addEventListener("click", function () {
      showError("");
      api("/api/posts/preview", {
        method: "POST",
        body: JSON.stringify({ body: bodyEl.value })
      }).then(function (res) {
        if (!res.ok) {
          showError("Preview failed.");
          return;
        }
        return res.json().then(function (out) {
          previewEl.innerHTML = out.html || "";
        });
      }).catch(function () {
        showError("Preview failed.");
      });
    });

    document.getElementById("publish-btn").addEventListener("click", function () {
      if (mode === "new") {
        showError("Save the post first.");
        return;
      }
      api("/api/posts/" + encodeURIComponent(slugEl.value) + "/publish", {
        method: "POST",
        body: "{}"
      }).then(function (res) {
        if (!res.ok) showError("Publish failed.");
      });
    });

    document.getElementById("unpublish-btn").addEventListener("click", function () {
      if (mode === "new") {
        showError("Save the post first.");
        return;
      }
      api("/api/posts/" + encodeURIComponent(slugEl.value) + "/unpublish", {
        method: "POST",
        body: "{}"
      }).then(function (res) {
        if (!res.ok) showError("Unpublish failed.");
      });
    });
  }

  initLogin();
  initList();
  initEditor();
})();

// shortly — browser client. No framework, no build step.

// ---------------------------------------------------------------------------
// Session
//
// The access token lives in this variable and NOWHERE else. Not localStorage,
// not sessionStorage, not a readable cookie — any of those can be read by
// injected JavaScript, and a stolen token works until it expires.
//
// Surviving a page reload is the refresh cookie's job: it is HttpOnly, so this
// script cannot read it, but the browser still sends it to /v1/auth/refresh.
// That is the whole point of the split.
// ---------------------------------------------------------------------------
let accessToken = null;
let currentEmail = null;
let refreshTimer = null;

const $ = (sel) => document.querySelector(sel);

// ---------------------------------------------------------------------------
// API helper
// ---------------------------------------------------------------------------

async function api(method, path, body, { auth = false } = {}) {
  const headers = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (auth && accessToken) headers["Authorization"] = `Bearer ${accessToken}`;

  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    // Send cookies, so the refresh endpoint receives the HttpOnly cookie.
    credentials: "same-origin",
  });

  // 401 on an authenticated call usually means the access token just expired.
  // Refresh once and retry, so the user never notices the 15 minute lifetime.
  if (res.status === 401 && auth && accessToken) {
    const refreshed = await refresh();
    if (refreshed) return api(method, path, body, { auth });
  }

  if (res.status === 204) return null;

  const text = await res.text();
  const data = text ? JSON.parse(text) : null;

  if (!res.ok) throw new Error(data?.error || `request failed (${res.status})`);
  return data;
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

function decodeToken(token) {
  try {
    // A JWT is base64url(header).base64url(payload).signature — signed, not
    // encrypted, so anyone holding it can read the claims. Never put secrets in.
    const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(atob(payload));
  } catch {
    return null;
  }
}

function startSession(data) {
  accessToken = data.access_token;
  const claims = decodeToken(accessToken);
  currentEmail = claims?.email ?? null;

  // Refresh a minute before expiry so a request never lands on a dead token.
  clearTimeout(refreshTimer);
  const wait = Math.max((data.expires_in - 60) * 1000, 10_000);
  refreshTimer = setTimeout(refresh, wait);

  render();
  loadLinks();
}

function endSession() {
  accessToken = null;
  currentEmail = null;
  clearTimeout(refreshTimer);
  render();
}

async function refresh() {
  try {
    const data = await api("POST", "/v1/auth/refresh");
    startSession(data);
    return true;
  } catch {
    endSession();
    return false;
  }
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

function render() {
  const signedIn = accessToken !== null;

  $("#signed-in").classList.toggle("hidden", !signedIn);
  $("#signed-out").classList.toggle("hidden", signedIn);
  $("#dashboard").classList.toggle("hidden", !signedIn);

  if (signedIn) {
    $("#account-email").textContent = currentEmail ?? "";
    $("#avatar").textContent = (currentEmail ?? "?")[0].toUpperCase();
    $("#shorten-hint").textContent = "Signed in — your links are saved below.";
  } else {
    $("#shorten-hint").textContent =
      "Works without an account — sign in to keep track of them.";
  }
}

// textContent everywhere, never innerHTML with user data: a target URL is
// attacker-controlled, and innerHTML would execute whatever is in it.
function linkCard(link) {
  const card = document.createElement("div");
  card.className = "link-card";

  const code = document.createElement("span");
  code.className = "link-code";
  code.textContent = "/" + link.code;

  const target = document.createElement("span");
  target.className = "link-target";
  target.textContent = link.target_url;
  target.title = link.target_url;

  const date = document.createElement("span");
  date.className = "link-date";
  date.textContent = new Date(link.created_at).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });

  const copy = document.createElement("button");
  copy.className = "icon-btn";
  copy.textContent = "Copy";
  copy.onclick = () => copyText(link.short_url, copy);

  const del = document.createElement("button");
  del.className = "icon-btn danger";
  del.textContent = "Delete";
  del.onclick = () => deleteLink(link.code, card);

  card.append(code, target, date, copy, del);
  return card;
}

async function loadLinks() {
  if (!accessToken) return;

  try {
    const { links } = await api("GET", "/v1/links", undefined, { auth: true });

    const list = $("#link-list");
    list.textContent = "";
    links.forEach((link, i) => {
      const card = linkCard(link);
      card.style.animationDelay = `${i * 35}ms`;
      list.append(card);
    });

    $("#link-count").textContent = links.length
      ? `${links.length} link${links.length === 1 ? "" : "s"}`
      : "";
    $("#empty-state").classList.toggle("hidden", links.length > 0);
  } catch (err) {
    toast(err.message, "error");
  }
}

async function deleteLink(code, card) {
  try {
    await api("DELETE", `/v1/links/${encodeURIComponent(code)}`, undefined, { auth: true });
    card.remove();
    toast("Link deleted", "ok");
    loadLinks();
  } catch (err) {
    toast(err.message, "error");
  }
}

// ---------------------------------------------------------------------------
// Shorten
// ---------------------------------------------------------------------------

$("#shorten-form").addEventListener("submit", async (e) => {
  e.preventDefault();

  const button = e.target.querySelector("button");
  const hint = $("#shorten-hint");
  const url = $("#url-input").value.trim();
  if (!url) return;

  button.classList.add("loading");
  button.disabled = true;
  hint.classList.remove("error");

  try {
    const link = await api("POST", "/v1/links", { url }, { auth: true });

    const anchor = $("#result-url");
    anchor.textContent = link.short_url;
    anchor.href = link.short_url;
    $("#result").classList.remove("hidden");
    $("#url-input").value = "";

    if (accessToken) loadLinks();
  } catch (err) {
    hint.textContent = err.message;
    hint.classList.add("error");
  } finally {
    button.classList.remove("loading");
    button.disabled = false;
  }
});

$("#result-copy").addEventListener("click", (e) =>
  copyText($("#result-url").textContent, e.currentTarget)
);

async function copyText(text, button) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    toast("Could not access the clipboard", "error");
    return;
  }
  const original = button.textContent;
  button.textContent = "Copied";
  button.classList.add("done");
  setTimeout(() => {
    button.textContent = original;
    button.classList.remove("done");
  }, 1400);
}

// ---------------------------------------------------------------------------
// Auth modal
// ---------------------------------------------------------------------------

let authMode = "login";

function openAuth(mode) {
  authMode = mode;
  const registering = mode === "register";

  $("#auth-title").textContent = registering ? "Create your account" : "Welcome back";
  $("#auth-sub").textContent = registering
    ? "Twelve characters or more, please."
    : "Sign in to keep your links together.";
  $("#auth-submit-label").textContent = registering ? "Create account" : "Sign in";
  $("#switch-text").textContent = registering ? "Already have an account?" : "New here?";
  $("#auth-switch").textContent = registering ? "Sign in" : "Create an account";
  $("#auth-error").classList.add("hidden");
  $("#meter-wrap, #pw-meter") && ($("#pw-meter").style.width = "0");

  $("#auth-backdrop").classList.remove("hidden");
  $("#auth-email").focus();
}

function closeAuth() {
  $("#auth-backdrop").classList.add("hidden");
  $("#auth-form").reset();
  $("#pw-meter").style.width = "0";
}

document.querySelectorAll("[data-auth-open]").forEach((el) =>
  el.addEventListener("click", () => openAuth(el.dataset.authOpen))
);
$("#auth-close").addEventListener("click", closeAuth);
$("#auth-switch").addEventListener("click", () =>
  openAuth(authMode === "login" ? "register" : "login")
);
$("#auth-backdrop").addEventListener("click", (e) => {
  if (e.target === $("#auth-backdrop")) closeAuth();
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closeAuth();
});

// A rough strength hint. Purely cosmetic — the server enforces the real rule.
$("#auth-password").addEventListener("input", (e) => {
  const v = e.target.value;
  let score = Math.min(v.length / 16, 1) * 70;
  if (/[A-Z]/.test(v)) score += 10;
  if (/[0-9]/.test(v)) score += 10;
  if (/[^A-Za-z0-9]/.test(v)) score += 10;
  score = Math.min(score, 100);

  const meter = $("#pw-meter");
  meter.style.width = score + "%";
  meter.style.background =
    score < 45 ? "var(--danger)" : score < 75 ? "#f0b429" : "var(--accent-2)";
});

$("#auth-form").addEventListener("submit", async (e) => {
  e.preventDefault();

  const button = e.target.querySelector("button[type=submit]");
  const errorBox = $("#auth-error");
  const email = $("#auth-email").value.trim();
  const password = $("#auth-password").value;

  button.classList.add("loading");
  button.disabled = true;
  errorBox.classList.add("hidden");

  try {
    if (authMode === "register") {
      await api("POST", "/v1/auth/register", { email, password });
    }
    const session = await api("POST", "/v1/auth/login", { email, password });

    startSession(session);
    closeAuth();
    toast(authMode === "register" ? "Account created" : "Signed in", "ok");
  } catch (err) {
    errorBox.textContent = err.message;
    errorBox.classList.remove("hidden");
  } finally {
    button.classList.remove("loading");
    button.disabled = false;
  }
});

$("#logout").addEventListener("click", async () => {
  try {
    await api("POST", "/v1/auth/logout");
  } catch {
    // Logging out locally matters more than the server acknowledging it.
  }
  endSession();
  toast("Signed out", "ok");
});

// ---------------------------------------------------------------------------
// Toasts
// ---------------------------------------------------------------------------

function toast(message, kind = "") {
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.textContent = message;
  $("#toasts").append(el);

  setTimeout(() => {
    el.classList.add("out");
    el.addEventListener("animationend", () => el.remove());
  }, 2600);
}

// ---------------------------------------------------------------------------
// Boot
//
// Try to restore a session from the HttpOnly refresh cookie. If there is none,
// or it has been revoked, we simply start signed out.
// ---------------------------------------------------------------------------
render();
refresh();

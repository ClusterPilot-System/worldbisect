const tokenKey = "worldbisect.token";
const state = { token: sessionStorage.getItem(tokenKey) || "" };

const qs = (selector) => document.querySelector(selector);
const escapeHTML = (value) => String(value ?? "")
  .replaceAll("&", "&amp;")
  .replaceAll("<", "&lt;")
  .replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;")
  .replaceAll("'", "&#039;");

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.token}`);
  headers.set("Accept", "application/json");
  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const body = await response.json();
      message = body.error?.message || message;
    } catch (_) {}
    throw new Error(message);
  }
  const type = response.headers.get("content-type") || "";
  return type.includes("application/json") ? response.json() : response.text();
}

function setStatus(message, error = false) {
  const node = qs("#status");
  node.textContent = message;
  node.classList.toggle("error", error);
}

function tableRows(values, columns) {
  if (!values.length) return `<tr><td colspan="${columns.length}">No records</td></tr>`;
  return values.map((value) => `<tr>${columns.map((column) => `<td>${escapeHTML(column(value))}</td>`).join("")}</tr>`).join("");
}

async function refresh() {
  try {
    setStatus("Loading…");
    const [version, captures, analyses, jobs] = await Promise.all([
      api("/api/v1/version"),
      api("/api/v1/captures?limit=20"),
      api("/api/v1/analyses?limit=20"),
      api("/api/v1/jobs?limit=20"),
    ]);
    qs("#version").textContent = version.version;
    qs("#capture-count").textContent = captures.length;
    qs("#analysis-count").textContent = analyses.length;
    qs("#job-count").textContent = jobs.length;
    qs("#captures tbody").innerHTML = tableRows(captures, [
      (v) => v.id,
      (v) => v.label || "—",
      (v) => v.oracle_result?.passed,
      (v) => v.created_at,
    ]);
    qs("#analyses tbody").innerHTML = tableRows(analyses, [
      (v) => v.id,
      (v) => v.status,
      (v) => (v.causal_factors || []).length,
      (v) => v.created_at,
    ]);
    qs("#jobs tbody").innerHTML = tableRows(jobs, [
      (v) => v.id,
      (v) => v.type,
      (v) => v.state,
      (v) => v.updated_at,
    ]);
    qs("#login").hidden = true;
    qs("#dashboard").hidden = false;
    setStatus("Connected");
  } catch (error) {
    qs("#dashboard").hidden = true;
    qs("#login").hidden = false;
    setStatus(error.message, true);
  }
}

qs("#token-form").addEventListener("submit", (event) => {
  event.preventDefault();
  state.token = qs("#token").value.trim();
  if (!state.token) return;
  sessionStorage.setItem(tokenKey, state.token);
  refresh();
});

qs("#logout").addEventListener("click", () => {
  sessionStorage.removeItem(tokenKey);
  state.token = "";
  location.reload();
});

qs("#refresh").addEventListener("click", refresh);

if (state.token) refresh();

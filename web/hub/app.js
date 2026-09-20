"use strict";

(() => {
  const statuses = new Set(["PROVEN", "SUPPORTED", "CORRELATED", "UNPROVEN"]);

  // A stored report is untrusted input, including its outbound links. Bind the
  // run URL to the report's own repository and exclude credentials and redirects.
  function safeRunURL(value, repository) {
    if (typeof value !== "string" || typeof repository !== "string") return null;
    if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) return null;
    try {
      const url = new URL(value);
      if (url.protocol !== "https:" || url.hostname !== "github.com" || url.port || url.username || url.password || url.search || url.hash) return null;
      const prefix = `/${repository}/actions/runs/`;
      if (!url.pathname.startsWith(prefix) || !/^[0-9]+$/.test(url.pathname.slice(prefix.length))) return null;
      return url.href;
    } catch {
      return null;
    }
  }

  function filterReports(reports, repository, status, query) {
    const needle = query.trim().toLowerCase();
    return reports.filter((report) => (!repository || report.repository === repository)
      && (!status || report.status === status)
      && (!needle || [report.repository, report.check_name, report.finding, report.commit_sha, report.next_step]
        .some((value) => typeof value === "string" && value.toLowerCase().includes(needle))));
  }

  function cancelled() {
    const error = new Error("Session changed.");
    error.name = "AbortError";
    return error;
  }

  // The credential has no persistence path. Epoch checks are still necessary
  // when fetch abort is too late (for example while a JSON body is resolving).
  function createClient(onUnauthorized, fetcher = globalThis.fetch) {
    let token = "";
    let epoch = 0;
    const pending = new Set();
    function disconnect() {
      token = "";
      epoch += 1;
      for (const controller of pending) controller.abort();
      pending.clear();
    }
    function connect(value) {
      disconnect();
      token = value;
    }
    async function request(path, method = "GET") {
      if (!token) throw cancelled();
      if (!path.startsWith("/api/v1/")) throw new Error("Invalid API route.");
      const started = epoch;
      const controller = new AbortController();
      pending.add(controller);
      try {
        const response = await fetcher(path, {
          method,
          headers: { Authorization: `Bearer ${token}`, Accept: "application/json" },
          credentials: "omit",
          cache: "no-store",
          redirect: "error",
          signal: controller.signal,
        });
        if (started !== epoch) throw cancelled();
        if (response.status === 401) {
          disconnect();
          onUnauthorized();
          throw new Error("Your access token was rejected. Connect again with a valid workspace token.");
        }
        if (response.status === 204) return null;
        let data;
        try {
          data = await response.json();
        } catch {
          if (started !== epoch) throw cancelled();
          throw new Error(`The hub returned an unreadable response (${response.status}).`);
        }
        if (started !== epoch) throw cancelled();
        if (!response.ok) {
          const message = typeof data?.error === "string" ? data.error.slice(0, 400) : `Request failed (${response.status}).`;
          throw new Error(message);
        }
        return data;
      } finally {
        pending.delete(controller);
      }
    }
    return { connect, disconnect, request };
  }

  // Export security-sensitive helpers for the dependency-free Node tests.
  if (typeof module !== "undefined" && module.exports) {
    module.exports = { safeRunURL, filterReports, createClient };
  }
  if (typeof document === "undefined") return;

  const get = (id) => document.getElementById(id);
  let reports = [];
  let selected = null;
  let permission = "read";
  let listVersion = 0;
  let detailVersion = 0;
  let connected = false;
  const client = createClient(() => reset("Your session expired or its token was rejected. Please connect again.", true));

  function notice(message, error = false) {
    get("notice").textContent = message;
    get("notice").classList.toggle("error", error);
    get("notice").hidden = !message;
  }

  function reset(message = "Disconnected. This tab's token and reports have been cleared.", error = false) {
    client.disconnect();
    connected = false;
    listVersion += 1;
    detailVersion += 1;
    reports = [];
    selected = null;
    permission = "read";
    get("token").value = "";
    get("report-list").replaceChildren();
    get("repository-filter").replaceChildren(new Option("All repositories", ""));
    get("search").value = "";
    get("status-filter").value = "";
    get("workspace-label").textContent = "";
    get("report-count").textContent = "0";
    get("list-status").textContent = "";
    clearDetail();
    get("dashboard").hidden = true;
    get("session-controls").hidden = true;
    get("login").hidden = false;
    get("connect").disabled = false;
    get("connect").textContent = "Open workspace →";
    get("refresh").disabled = false;
    notice(message, error);
  }

  function clearDetail(message = "Select a report") {
    selected = null;
    get("detail-content").hidden = true;
    get("detail-empty").hidden = false;
    get("detail-empty").querySelector("h2").textContent = message;
    for (const id of ["detail-repository", "detail-status", "detail-title", "detail-meta", "detail-finding", "detail-tested", "detail-experiments", "detail-confidence", "detail-next", "detail-error"]) get(id).textContent = "";
    get("detail-status").className = "status-badge";
    get("detail-status").removeAttribute("title");
    get("run-link").hidden = true;
    get("run-link").removeAttribute("href");
    get("delete").hidden = true;
    get("delete-confirm").hidden = true;
    get("detail-error").hidden = true;
    get("delete-yes").disabled = false;
  }

  const value = (text, fallback = "Not included in this CI summary.") => typeof text === "string" && text.trim() ? text : fallback;
  const statusOf = (report) => statuses.has(report.status) ? report.status : "UNPROVEN";
  function dateOf(input) {
    const date = new Date(input);
    return Number.isNaN(date.getTime()) ? "Date unavailable" : date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  }
  function badge(node, report) {
    const status = statusOf(report);
    node.className = `status-badge status-${status.toLowerCase()}`;
    node.textContent = status;
    node.title = `CI-reported confidence: ${status}. Not independently verified by the hub.`;
  }
  function element(tag, className, text) {
    const node = document.createElement(tag);
    node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function renderList() {
    const filtered = filterReports(reports, get("repository-filter").value, get("status-filter").value, get("search").value);
    get("report-count").textContent = String(reports.length);
    get("list-status").textContent = `${filtered.length} of ${reports.length} reports`;
    get("report-list").replaceChildren();
    for (const report of filtered) {
      const button = element("button", "report-button");
      button.type = "button";
      button.setAttribute("aria-pressed", String(selected?.id === report.id));
      const top = element("span", "report-row-top");
      const status = element("span", "status-badge");
      badge(status, report);
      top.append(element("span", "report-repository", value(report.repository, "Repository unavailable")), status);
      button.append(top,
        element("span", "report-title", value(report.check_name, "CI check")),
        element("span", "report-finding", value(report.finding)),
        element("span", "report-meta", `${dateOf(report.created_at)} · ${value(report.commit_sha, "No commit").slice(0, 12)}`));
      button.addEventListener("click", () => openReport(report.id));
      get("report-list").append(button);
    }
    get("empty-state").hidden = filtered.length > 0;
    get("empty-title").textContent = reports.length ? "No matching reports" : "Your first report starts in CI";
    get("empty-description").textContent = reports.length ? "Try a different search, repository, or confidence filter." : "Publish a reviewed summary with scripts/publish-hub-report.py, then refresh this workspace.";
  }

  async function loadReports() {
    const requestVersion = ++listVersion;
    get("refresh").disabled = true;
    get("list-status").textContent = "Loading reports…";
    try {
      const data = await client.request("/api/v1/reports");
      if (requestVersion !== listVersion) return;
      if (!Array.isArray(data?.reports)) throw new Error("The hub returned an invalid report list.");
      reports = data.reports.filter((report) => report && typeof report.id === "string" && report.id);
      const previousFilter = get("repository-filter").value;
      const repositories = [...new Set(reports.map((report) => report.repository).filter((repo) => typeof repo === "string"))].sort();
      get("repository-filter").replaceChildren(new Option("All repositories", ""), ...repositories.map((repo) => new Option(repo, repo)));
      if (repositories.includes(previousFilter)) get("repository-filter").value = previousFilter;
      if (selected && !reports.some((report) => report.id === selected.id)) {
        detailVersion += 1;
        clearDetail();
      }
      renderList();
    } catch (error) {
      if (error.name !== "AbortError" && requestVersion === listVersion) {
        get("list-status").textContent = "Reports could not be refreshed.";
        throw error;
      }
    } finally {
      if (requestVersion === listVersion) get("refresh").disabled = false;
    }
  }

  const confidence = {
    PROVEN: "CI reports PROVEN within its declared proof boundary. Inspect the CI run for stable baselines, both intervention directions, and minimality checks.",
    SUPPORTED: "CI reports supporting evidence, but does not claim a complete proof.",
    CORRELATED: "CI reports an association. A causal change has not been established.",
    UNPROVEN: "CI reports that a cause was not established. Check reproduction and evidence limits.",
  };

  async function openReport(id) {
    const requestVersion = ++detailVersion;
    clearDetail("Loading report…");
    try {
      const data = await client.request(`/api/v1/reports/${encodeURIComponent(id)}`);
      if (requestVersion !== detailVersion) return;
      const report = data?.report || data;
      if (!report || report.id !== id) throw new Error("The hub returned an invalid report.");
      selected = report;
      get("detail-empty").hidden = true;
      get("detail-content").hidden = false;
      get("detail-repository").textContent = value(report.repository, "Repository unavailable");
      badge(get("detail-status"), report);
      get("detail-title").textContent = value(report.check_name, "CI check");
      get("detail-meta").textContent = `${dateOf(report.created_at)} · commit ${value(report.commit_sha, "unavailable").slice(0, 12)}`;
      get("detail-finding").textContent = value(report.finding);
      get("detail-tested").textContent = value(report.tested);
      get("detail-experiments").textContent = Number.isSafeInteger(report.experiments) && report.experiments >= 0 ? `${report.experiments} experiments reported by CI` : "Experiment count not included.";
      get("detail-confidence").textContent = confidence[statusOf(report)];
      get("detail-next").textContent = value(report.next_step);
      const url = safeRunURL(report.run_url, report.repository);
      if (url) {
        get("run-link").href = url;
        get("run-link").hidden = false;
      }
      get("delete").hidden = permission !== "write";
      renderList();
      get("detail-title").focus({ preventScroll: true });
    } catch (error) {
      if (error.name !== "AbortError" && requestVersion === detailVersion) {
        clearDetail("Report could not be loaded");
        notice(error.message, true);
        renderList();
      }
    }
  }

  get("token-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const token = get("token").value.trim();
    if (!token) return;
    notice("");
    get("connect").disabled = true;
    get("connect").textContent = "Connecting…";
    client.connect(token);
    get("token").value = "";
    try {
      const session = await client.request("/api/v1/session");
      if (typeof session?.workspace !== "string" || !["read", "write"].includes(session.permission)) throw new Error("The hub returned an invalid workspace session.");
      permission = session.permission;
      get("workspace-label").textContent = `${session.workspace} · ${permission === "write" ? "Read & write" : "Read only"}`;
      await loadReports();
      // An aborted list request must never re-open a disconnected workspace.
      if (!get("workspace-label").textContent) return;
      connected = true;
      get("login").hidden = true;
      get("dashboard").hidden = false;
      get("session-controls").hidden = false;
      get("search").focus({ preventScroll: true });
    } catch (error) {
      if (error.name !== "AbortError") reset(error.message, true);
    } finally {
      get("connect").disabled = false;
      get("connect").textContent = "Open workspace →";
    }
  });

  get("disconnect").addEventListener("click", () => { reset(); get("token").focus(); });
  get("refresh").addEventListener("click", async () => {
    notice("");
    try { await loadReports(); } catch (error) { if (connected) notice(error.message, true); }
  });
  for (const id of ["search", "repository-filter", "status-filter"]) get(id).addEventListener(id === "search" ? "input" : "change", renderList);
  get("delete").addEventListener("click", () => { get("delete-confirm").hidden = false; get("delete-cancel").focus(); });
  get("delete-cancel").addEventListener("click", () => { get("delete-confirm").hidden = true; get("delete").focus(); });
  get("delete-yes").addEventListener("click", async () => {
    if (!selected || permission !== "write") return;
    const id = selected.id;
    const requestVersion = detailVersion;
    get("delete-yes").disabled = true;
    get("detail-error").hidden = true;
    try {
      await client.request(`/api/v1/reports/${encodeURIComponent(id)}`, "DELETE");
      if (!connected) return;
      if (requestVersion === detailVersion) { detailVersion += 1; clearDetail(); }
      reports = reports.filter((report) => report.id !== id);
      renderList();
      notice("Report deleted. The original CI run is unchanged.");
      await loadReports();
    } catch (error) {
      if (error.name !== "AbortError" && connected) {
        if (requestVersion === detailVersion) {
          get("detail-error").textContent = error.message;
          get("detail-error").hidden = false;
        } else notice(error.message, true);
      }
    } finally {
      if (requestVersion === detailVersion) get("delete-yes").disabled = false;
    }
  });
  window.addEventListener("pagehide", () => reset(""));
})();

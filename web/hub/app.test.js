"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { safeRunURL, createClient, sessionAccess } = require("./app.js");

test("effective scopes authorize reading and deletion independently from role or legacy permission", () => {
  assert.deepEqual(sessionAccess({ workspace: "team", permission: "write", role: "publisher", scopes: ["reports:write"] }),
    { read: false, write: true, delete: false });
  assert.deepEqual(sessionAccess({ workspace: "team", permission: "write", role: "editor", scopes: ["reports:read", "reports:write"] }),
    { read: true, write: true, delete: false });
  assert.deepEqual(sessionAccess({ workspace: "team", permission: "write", role: "editor", scopes: ["reports:read"] }),
    { read: true, write: false, delete: false });
  assert.deepEqual(sessionAccess({ workspace: "team", scopes: ["reports:read", "reports:delete"] }),
    { read: true, write: false, delete: true });
  assert.deepEqual(sessionAccess({ workspace: "team", permission: "write", scopes: ["unknown:scope"] }),
    { read: false, write: false, delete: false });
});

test("missing or malformed scopes fail closed even when legacy permission grants write", () => {
  for (const scopes of [undefined, null, "reports:read", ["reports:read", 123]]) {
    assert.throws(() => sessionAccess({ workspace: "team", permission: "write", scopes }), /access scopes/);
  }
  assert.deepEqual(sessionAccess({ workspace: "team", permission: "write", scopes: [] }),
    { read: false, write: false, delete: false });
});

test("CI links are bound to the report repository and GitHub run route", () => {
  assert.equal(safeRunURL("https://github.com/team/project/actions/runs/123", "team/project"), "https://github.com/team/project/actions/runs/123");
  for (const url of [
    "javascript:alert(1)",
    "https://github.com.evil.example/team/project/actions/runs/123",
    "https://github.com@evil.example/team/project/actions/runs/123",
    "https://user:pass@github.com/team/project/actions/runs/123",
    "https://github.com/team/other/actions/runs/123",
    "http://github.com/team/project/actions/runs/123",
    "https://github.com/team/project/actions/runs/123?redirect=evil",
    "https://github.com/team/project/actions/runs/123#fragment",
    "https://github.com/team/project/actions/runs/../secrets",
    "https://github.com/team/project/actions/runs/123/jobs/99",
  ]) assert.equal(safeRunURL(url, "team/project"), null, url);
});

test("disconnect cancels pending requests and discards a late response body", async () => {
  let resolveBody;
  let options;
  const body = new Promise((resolve) => { resolveBody = resolve; });
  const client = createClient(() => assert.fail("should not expire"), async (_path, opts) => {
    options = opts;
    return { status: 200, ok: true, json: () => body };
  });
  client.connect("old-workspace-token");
  const request = client.request("/api/v1/reports");
  await Promise.resolve();
  client.disconnect();
  assert.equal(options.signal.aborted, true);
  assert.equal(options.headers.Authorization, "Bearer old-workspace-token");
  assert.equal(options.redirect, "error");
  assert.equal(options.credentials, "omit");
  resolveBody({ reports: [{ id: "private-old-report" }] });
  await assert.rejects(request, { name: "AbortError" });
  await assert.rejects(client.request("/api/v1/reports"), { name: "AbortError" });
});

test("a late 401 from the old workspace cannot disconnect a new session", async () => {
  let resolveOld;
  let calls = 0;
  let expirations = 0;
  const client = createClient(() => { expirations += 1; }, async (_path, options) => {
    calls += 1;
    if (calls === 1) return new Promise((resolve) => { resolveOld = resolve; });
    assert.equal(options.headers.Authorization, "Bearer new-token");
    return { status: 200, ok: true, json: async () => ({ workspace: "new" }) };
  });
  client.connect("old-token");
  const old = client.request("/api/v1/session");
  client.connect("new-token");
  resolveOld({ status: 401, ok: false });
  await assert.rejects(old, { name: "AbortError" });
  assert.deepEqual(await client.request("/api/v1/session"), { workspace: "new" });
  assert.equal(expirations, 0);
});

test("a current 401 clears authentication and invokes the protected-data reset", async () => {
  let expirations = 0;
  const client = createClient(() => { expirations += 1; }, async () => ({ status: 401, ok: false }));
  client.connect("expired-token");
  await assert.rejects(client.request("/api/v1/reports"), /token was rejected/);
  assert.equal(expirations, 1);
  await assert.rejects(client.request("/api/v1/reports"), { name: "AbortError" });
});

// A small DOM/event harness executes the real application handlers. It verifies
// denied requests and data clearing, not just whether a button looks hidden.
function dashboardHarness(initialSession) {
  class Node {
    constructor() {
      this.children = [];
      this.events = {};
      this.value = "";
      this.textContent = "";
      this.hidden = false;
      this.disabled = false;
      this.classList = { toggle() {} };
      this.attributes = {};
    }
    addEventListener(name, fn) { this.events[name] = fn; }
    async emit(name) { await this.events[name]?.({ preventDefault() {} }); }
    replaceChildren(...nodes) { this.children = nodes; }
    append(...nodes) { this.children.push(...nodes); }
    setAttribute(name, val) { this.attributes[name] = val; }
    removeAttribute(name) { delete this.attributes[name]; }
    querySelector() { this.heading ||= new Node(); return this.heading; }
    focus() {}
  }
  const nodes = new Map();
  function get(id) {
    if (!nodes.has(id)) nodes.set(id, new Node());
    return nodes.get(id);
  }
  for (const id of ["dashboard", "session-controls", "detail-content", "delete", "delete-confirm"]) get(id).hidden = true;
  const state = { session: initialSession, readDenied: false, deleted: false, calls: [] };
  const report = {
    id: "report-1", repository: "team/project", check_name: "build", status: "PROVEN",
    finding: "Selected configuration differs", tested: "Both directions", next_step: "Review config",
    created_at: "2026-09-20T12:00:00Z", experiments: 9,
  };
  async function fetcher(path, options) {
    state.calls.push({ path, method: options.method });
    let status = 200;
    let data;
    if (path === "/api/v1/session") data = state.session;
    else if (options.method === "DELETE") { state.deleted = true; status = 204; }
    else if (state.readDenied) { status = 403; data = { error: "reports:read required" }; }
    else if (path === "/api/v1/reports") data = { reports: state.deleted ? [] : [report] };
    else data = report;
    return { status, ok: status < 400, json: async () => data };
  }
  vm.runInNewContext(fs.readFileSync(require.resolve("./app.js"), "utf8"), {
    document: { getElementById: get, createElement: () => new Node() },
    window: { addEventListener() {} },
    fetch: fetcher, URL, AbortController,
    Option: function Option(text, val) { const node = new Node(); node.textContent = text; node.value = val; return node; },
  });
  return {
    get, state,
    async connect() { get("token").value = "test-credential"; await get("token-form").emit("submit"); },
    async openFirst() { await get("report-list").children[0].emit("click"); },
  };
}

test("publisher credentials cannot request or display report lists", async () => {
  const ui = dashboardHarness({ workspace: "team", permission: "write", subject_id: "ci", role: "publisher", scopes: ["reports:write"] });
  await ui.connect();
  assert.deepEqual(ui.state.calls, [{ path: "/api/v1/session", method: "GET" }]);
  assert.equal(ui.get("dashboard").hidden, true);
  assert.equal(ui.get("login").hidden, false);
  assert.equal(ui.get("report-list").children.length, 0);
  assert.match(ui.get("notice").textContent, /publishing credential cannot read/);
  await ui.get("refresh").emit("click");
  assert.equal(ui.state.calls.length, 1, "even a dispatched hidden refresh must not read reports");
});

test("an editor credential limited to read and publish cannot trigger deletion", async () => {
  const ui = dashboardHarness({ workspace: "team", permission: "write", subject_id: "alice", role: "editor", scopes: ["reports:read", "reports:write"] });
  await ui.connect();
  await ui.openFirst();
  assert.equal(ui.get("dashboard").hidden, false);
  assert.equal(ui.get("delete").hidden, true);
  assert.match(ui.get("workspace-label").textContent, /alice · editor · Read & publish/);
  await ui.get("delete").emit("click");
  await ui.get("delete-yes").emit("click");
  assert.equal(ui.get("delete-confirm").hidden, true);
  assert.equal(ui.state.calls.some((call) => call.method === "DELETE"), false);
});

test("deletion is available only with an explicit effective delete scope", async () => {
  const ui = dashboardHarness({ workspace: "team", permission: "write", subject_id: "alice", role: "editor", scopes: ["reports:read", "reports:delete"] });
  await ui.connect();
  await ui.openFirst();
  assert.equal(ui.get("delete").hidden, false);
  await ui.get("delete").emit("click");
  assert.equal(ui.get("delete-confirm").hidden, false);
  await ui.get("delete-yes").emit("click");
  assert.equal(ui.state.deleted, true);
  assert.equal(ui.get("report-list").children.length, 0);
});

test("a replacement session without read scope clears the previous workspace", async () => {
  const ui = dashboardHarness({ workspace: "old-team", permission: "read", scopes: ["reports:read"] });
  await ui.connect();
  await ui.openFirst();
  ui.state.session = { workspace: "other-team", permission: "write", scopes: ["reports:write"] };
  await ui.connect();
  assert.equal(ui.get("report-list").children.length, 0);
  assert.equal(ui.get("detail-finding").textContent, "");
  assert.equal(ui.get("workspace-label").textContent, "");
  assert.equal(ui.get("dashboard").hidden, true);
  assert.equal(ui.state.calls.filter((call) => call.path === "/api/v1/reports").length, 1);
});

test("a report-read denial clears previously displayed summaries and reconnects", async () => {
  const ui = dashboardHarness({ workspace: "team", permission: "read", scopes: ["reports:read"] });
  await ui.connect();
  await ui.openFirst();
  ui.state.readDenied = true;
  await ui.get("refresh").emit("click");
  assert.equal(ui.get("dashboard").hidden, true);
  assert.equal(ui.get("login").hidden, false);
  assert.equal(ui.get("report-list").children.length, 0);
  assert.equal(ui.get("detail-finding").textContent, "");
  assert.match(ui.get("notice").textContent, /access was denied/);
});

"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { safeRunURL, createClient } = require("./app.js");

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

"use strict";

// Development/CI only. Playwright is deliberately not a product dependency.
// Run from the repository root: node web/hub/browser-check.cjs
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");

const output = path.resolve("build/hub-browser");
const reports = [
  {
    id: "a".repeat(32), repository: "example/service", check_name: "Configuration check",
    commit_sha: "b".repeat(40), created_at: "2026-09-20T12:00:00Z",
    run_url: "https://github.com/example/service/actions/runs/42", status: "PROVEN",
    finding: "<img src=x onerror=alert(1)> A changed service configuration reproduces the failing check.",
    tested: "The configured check ran in both directions with stable good and bad baselines.",
    next_step: "Review the changed configuration before merging.", experiments: 9,
  },
  {
    id: "c".repeat(32), repository: "example/worker", check_name: "Worker check",
    commit_sha: "d".repeat(40), created_at: "2026-09-19T12:00:00Z",
    run_url: "javascript:alert(1)", status: "UNPROVEN",
    finding: "The baseline did not reproduce.", tested: "The worker check ran twice.",
    next_step: "Check the reproduction environment.", experiments: 2,
  },
];
const sessions = {
  "Bearer test-writer-token": {
    workspace: "example-team", permission: "write", subject_id: "alice", kind: "user",
    credential_id: "writer-key", role: "editor", scopes: ["reports:read", "reports:write", "reports:delete"],
  },
  "Bearer test-reader-token": {
    workspace: "example-team", permission: "read", subject_id: "bob", kind: "user",
    credential_id: "reader-key", role: "viewer", scopes: ["reports:read"],
  },
  "Bearer test-limited-editor-token": {
    workspace: "example-team", permission: "write", subject_id: "charlie", kind: "user",
    credential_id: "limited-editor-key", role: "editor", scopes: ["reports:read", "reports:write"],
  },
  "Bearer test-publisher-token": {
    workspace: "example-team", permission: "write", subject_id: "build-ci", kind: "service",
    credential_id: "publisher-key", role: "publisher", scopes: ["reports:write"],
  },
};
let expired = false;
const calls = [];
const server = http.createServer((request, response) => {
  response.setHeader("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'");
  response.setHeader("Cache-Control", "no-store");
  if (request.url.startsWith("/api/")) {
    response.setHeader("Content-Type", "application/json");
    const authorization = request.headers.authorization;
    calls.push({ path: request.url, method: request.method, authorization });
    const session = sessions[authorization];
    if (expired || !session) {
      response.writeHead(401).end(JSON.stringify({ error: "Expired or invalid token" }));
      return;
    }
    if (request.url === "/api/v1/session") {
      response.end(JSON.stringify(session));
    } else {
      const requiredScope = request.method === "DELETE" ? "reports:delete" : "reports:read";
      if (!session.scopes.includes(requiredScope)) {
        response.writeHead(403).end(JSON.stringify({ error: `${requiredScope} required` }));
        return;
      }
      if (request.url === "/api/v1/reports") {
        response.end(JSON.stringify({ reports }));
        return;
      }
      const report = reports.find((item) => request.url === `/api/v1/reports/${item.id}`);
      if (report) response.end(JSON.stringify(report));
      else response.writeHead(404).end(JSON.stringify({ error: "Report not found" }));
    }
    return;
  }
  const file = { "/": "index.html", "/app.js": "app.js", "/style.css": "style.css" }[request.url];
  if (!file) { response.writeHead(404).end(); return; }
  response.setHeader("Content-Type", file.endsWith(".js") ? "text/javascript" : file.endsWith(".css") ? "text/css" : "text/html");
  response.end(fs.readFileSync(path.join(__dirname, file)));
});

async function main() {
  fs.mkdirSync(output, { recursive: true });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  let browser;
  let page;
  try {
    browser = await chromium.launch({ headless: true });
    page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
    const errors = [];
    const dialogs = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("dialog", async (dialog) => { dialogs.push(dialog.message()); await dialog.dismiss(); });
    await page.goto(`http://127.0.0.1:${server.address().port}`);
    await page.screenshot({ path: path.join(output, "desktop-login.png"), fullPage: true });

    async function submitToken(token) {
      await page.getByLabel("Workspace access token").fill(token);
      await page.getByRole("button", { name: "Open workspace" }).click();
    }
    async function connect(token) {
      await submitToken(token);
      await page.locator("#dashboard").waitFor({ state: "visible" });
    }
    async function selectReport(name) {
      await page.getByRole("button", { name: new RegExp(name) }).click();
      await page.locator("#detail-content").waitFor({ state: "visible" });
      assert.equal(await page.locator("#detail-title").textContent(), name);
    }
    async function assertNoOverflow() {
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "page must fit its viewport");
    }

    await connect("test-writer-token");
    assert.match(await page.locator("#workspace-label").textContent(), /alice · editor · Read, publish & delete/);
    assert.equal(await page.getByLabel("Workspace access token").inputValue(), "");
    assert.equal(await page.locator(".report-button").count(), 2);
    await page.getByLabel("Repository", { exact: true }).selectOption("example/worker");
    assert.equal(await page.locator(".report-button").count(), 1);
    await page.getByLabel("Reported confidence", { exact: true }).selectOption("PROVEN");
    assert.equal(await page.locator("#empty-title").textContent(), "No matching reports");
    await page.getByLabel("Reported confidence", { exact: true }).selectOption("UNPROVEN");
    await selectReport("Worker check");
    assert.equal(await page.locator("#run-link").isVisible(), false, "untrusted run URL must not become a link");
    await page.getByLabel("Repository", { exact: true }).selectOption("");
    await page.getByLabel("Reported confidence", { exact: true }).selectOption("");
    await page.getByLabel("Search reports").fill("no-matching-report");
    assert.equal(await page.locator(".report-button").count(), 0);
    await page.getByLabel("Search reports").fill("");
    await selectReport("Configuration check");
    assert.equal(await page.locator("#detail-finding img").count(), 0);
    assert.match(await page.locator("#detail-finding").textContent(), /<img/);
    assert.equal(await page.locator("#run-link").getAttribute("href"), reports[0].run_url);
    assert.equal(await page.locator("#delete").isVisible(), true);
    await assertNoOverflow();
    await page.screenshot({ path: path.join(output, "desktop-reports.png"), fullPage: true });

    await page.setViewportSize({ width: 390, height: 844 });
    await assertNoOverflow();
    await page.screenshot({ path: path.join(output, "mobile-reports.png"), fullPage: true });
    await page.getByRole("button", { name: "Disconnect" }).click();
    assert.equal(await page.locator("#detail-finding").textContent(), "");
    assert.equal(await page.locator("#report-list").textContent(), "");
    assert.equal(await page.locator("#run-link").getAttribute("href"), null);
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);

    await connect("test-limited-editor-token");
    await selectReport("Configuration check");
    assert.match(await page.locator("#workspace-label").textContent(), /charlie · editor · Read & publish/);
    assert.equal(await page.locator("#delete").isVisible(), false, "editor role and write permission must not imply delete scope");
    // Dispatching hidden controls checks the handler guards as well as rendering.
    await page.locator("#delete").dispatchEvent("click");
    await page.locator("#delete-yes").dispatchEvent("click");
    assert.equal(await page.locator("#delete-confirm").isVisible(), false);
    assert.equal(calls.some((call) => call.authorization === "Bearer test-limited-editor-token" && call.method === "DELETE"), false);
    await assertNoOverflow();
    await page.getByRole("button", { name: "Disconnect" }).click();

    const publisherCallsBefore = calls.length;
    await submitToken("test-publisher-token");
    await page.locator("#notice").filter({ hasText: "publishing credential cannot read team reports" }).waitFor({ state: "visible" });
    assert.equal(await page.locator("#dashboard").isVisible(), false);
    assert.equal(await page.locator("#login").isVisible(), true);
    assert.equal(await page.locator("#report-list").textContent(), "");
    assert.equal(await page.locator("#detail-finding").textContent(), "");
    assert.equal(await page.locator("#workspace-label").textContent(), "");
    assert.equal(await page.getByLabel("Workspace access token").inputValue(), "");
    await page.locator("#refresh").dispatchEvent("click");
    assert.deepEqual(calls.slice(publisherCallsBefore).map((call) => [call.path, call.method]),
      [["/api/v1/session", "GET"]], "publisher must never send a list or detail request, including from hidden refresh");
    await assertNoOverflow();

    await connect("test-reader-token");
    assert.match(await page.locator("#workspace-label").textContent(), /bob · viewer · Read only/);
    await selectReport("Configuration check");
    assert.equal(await page.locator("#delete").isVisible(), false, "read-only access must not offer deletion");
    expired = true;
    await page.getByRole("button", { name: "Refresh reports" }).click();
    await page.locator("#login").waitFor({ state: "visible" });
    assert.equal(await page.locator("#report-list").textContent(), "");
    assert.equal(await page.locator("#detail-finding").textContent(), "");
    assert.equal(await page.locator("#workspace-label").textContent(), "");
    assert.equal(page.url().includes("token"), false);
    assert.deepEqual(errors, [], "no browser script errors");
    assert.deepEqual(dialogs, [], "report text must never execute");
    assert(calls.length >= 6 && calls.every((call) => !call.path.includes("token") && call.authorization?.startsWith("Bearer ")));
    console.log("PASS: hub browser login, filters, untrusted text and links, memory-only token, disconnect, effective read/delete scopes, limited editor, publisher denied without report requests, 401 cleanup, desktop/mobile layout.");
    console.log(`Screenshots: ${output}`);
  } catch (error) {
    if (page) await page.screenshot({ path: path.join(output, "failure.png"), fullPage: true }).catch(() => {});
    throw error;
  } finally {
    if (browser) await browser.close();
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => { console.error(error); process.exitCode = 1; });

'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const publish = require('./comment.cjs');
function fixture() {
  const calls = [];
  const pr = {number: 7, state: 'open', head: {sha: 'a'.repeat(40), repo: {full_name: 'owner/repo'}}};
  const comments = [];
  const args = {
    context: {repo: {owner: 'owner', repo: 'repo'}, eventName: 'pull_request', payload: {pull_request: pr}},
    env: {COMMENT_KEY: 'b'.repeat(32), SUMMARY_PATH: '/safe/summary.md', ARTIFACT_URL: 'https://github.com/owner/repo/actions/runs/12/artifacts/34'},
    core: {notice() {}}, readFile: () => '# Diagnosis\n**Finding:** config differs',
    github: {paginate: async () => comments, rest: {
      pulls: {get: async () => ({data: pr})},
      issues: {listComments() {}, updateComment: async body => calls.push(['update', body]),
        createComment: async body => calls.push(['create', body])},
    }},
  };
  return {args, pr, comments, calls};
}
test('creates useful bounded comment with evidence and head', async () => {
  const f = fixture(); await publish(f.args);
  assert.equal(f.calls[0][0], 'create');
  assert.match(f.calls[0][1].body, /Full diagnostic artifact/);
  assert.match(f.calls[0][1].body, /Commit:/);
});
test('updates only the matching GitHub Actions bot comment', async () => {
  const f = fixture(); const marker = `<!-- worldbisect-ci:${f.args.env.COMMENT_KEY} -->\n`;
  f.comments.push({id: 1, user: {login: 'person', type: 'User'}, body: marker},
    {id: 2, user: {login: 'other[bot]', type: 'Bot'}, body: marker},
    {id: 3, user: {login: 'github-actions[bot]', type: 'Bot'}, body: marker});
  await publish(f.args); assert.equal(f.calls[0][0], 'update'); assert.equal(f.calls[0][1].comment_id, 3);
});
test('check keys separate matrix variants', async () => {
  const f = fixture(); f.comments.push({id: 3, user: {login: 'github-actions[bot]', type: 'Bot'}, body: `<!-- worldbisect-ci:${'c'.repeat(32)} -->\n`});
  await publish(f.args); assert.equal(f.calls[0][0], 'create');
});
test('forks and pull_request_target do not read or write comments', async () => {
  for (const fork of [false, true]) {
    const f = fixture(); if (fork) f.pr.head.repo.full_name = 'fork/repo'; else f.args.context.eventName = 'pull_request_target';
    f.args.github.rest.pulls.get = () => {throw Error('must not call');};
    await publish(f.args); assert.equal(f.calls.length, 0);
  }
});
test('stale and closed PR results never publish', async () => {
  for (const change of [{state: 'closed'}, {head: {sha: 'new', repo: {full_name: 'owner/repo'}}}]) {
    const f = fixture(); f.args.github.rest.pulls.get = async () => ({data: {...f.pr, ...change}});
    await publish(f.args); assert.equal(f.calls.length, 0);
  }
});
test('rechecks the PR head after reading existing comments', async () => {
  const f = fixture(); let count = 0;
  f.args.github.rest.pulls.get = async () => ({data: ++count === 1 ? f.pr : {...f.pr, head: {...f.pr.head, sha: 'new'}}});
  await publish(f.args); assert.equal(f.calls.length, 0);
});
test('rejects arbitrary artifact links and bounds long summaries', async () => {
  const f = fixture(); f.args.env.ARTIFACT_URL = 'https://example.invalid/tracking'; f.args.readFile = () => 'x'.repeat(30000);
  await publish(f.args); assert.ok(f.calls[0][1].body.length < 8000); assert.doesNotMatch(f.calls[0][1].body, /tracking/);
});
test('API permission failure remains visible to optional action step', async () => {
  const f = fixture(); f.args.github.rest.issues.createComment = async () => {throw new Error('403');};
  await assert.rejects(publish(f.args), /403/);
});

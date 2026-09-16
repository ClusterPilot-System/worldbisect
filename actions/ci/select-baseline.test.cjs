'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const select = require('./select-baseline.cjs');

function fixture() {
  const now = Date.now();
  const run = {id: 41, workflow_id: 5, event: 'push', conclusion: 'success', head_branch: 'main',
    head_repository: {full_name: 'acme/test'}, head_sha: 'good', created_at: new Date(now - 60000).toISOString()};
  const artifact = {id: 7, name: 'expected', expired: false, size_in_bytes: 123,
    workflow_run: {id: 41, head_sha: 'good'}};
  const outputs = {};
  const calls = [];
  const args = {
    env: {RETENTION: '7', ARTIFACT_NAME: 'expected'},
    context: {repo: {owner: 'acme', repo: 'test'}, runId: 42, eventName: 'push',
      payload: {repository: {default_branch: 'main'}}},
    core: {notice() {}, setOutput: (k, v) => {outputs[k] = v;}},
    github: {rest: {actions: {
      getWorkflowRun: async () => ({data: {workflow_id: 5, created_at: new Date(now).toISOString()}}),
      listWorkflowRuns: async params => {calls.push(params); return {data: {workflow_runs: [run]}};},
      listWorkflowRunArtifacts: async () => ({data: {artifacts: [artifact]}}),
    }}},
  };
  return {args, run, artifact, outputs, calls};
}

test('selects exact successful trusted workflow and artifact', async () => {
  const f = fixture(); await select(f.args);
  assert.equal(f.outputs['run-id'], '41'); assert.equal(f.outputs.sha, 'good');
  assert.equal(f.outputs['artifact-id'], '7');
  assert.equal(f.calls[0].workflow_id, 5); assert.equal(f.calls[0].branch, 'main');
});
for (const [key, value] of [['event', 'pull_request'], ['conclusion', 'failure'], ['head_branch', 'feature'],
  ['workflow_id', 99], ['id', 42], ['created_at', '2000-01-01'], ['created_at', '2099-01-01'],
  ['head_repository', {full_name: 'fork/test'}]]) {
  test(`rejects incompatible run ${key}=${JSON.stringify(value)}`, async () => {
    const f = fixture(); f.run[key] = value; await select(f.args); assert.deepEqual(f.outputs, {});
  });
}
for (const [key, value] of [['name', 'other'], ['expired', true], ['size_in_bytes', 30 * 1024 * 1024],
  ['workflow_run', {id: 999, head_sha: 'good'}], ['workflow_run', {id: 41, head_sha: 'wrong'}]]) {
  test(`rejects incompatible artifact ${key}`, async () => {
    const f = fixture(); f.artifact[key] = value; await select(f.args); assert.deepEqual(f.outputs, {});
  });
}
test('fork PRs and pull_request_target never access raw baselines', async () => {
  for (const event of ['pull_request', 'pull_request_target']) {
    const f = fixture(); f.args.context.eventName = event;
    f.args.context.payload.pull_request = {head: {repo: {full_name: 'fork/test'}}};
    await select(f.args); assert.equal(f.calls.length, 0);
  }
});
test('same repository PR may diagnose, but cannot publish', async () => {
  const f = fixture(); f.args.context.eventName = 'pull_request';
  f.args.context.payload.pull_request = {head: {repo: {full_name: 'acme/test'}}};
  await select(f.args); assert.equal(f.outputs['run-id'], '41');
});
test('no artifact produces no guessed baseline', async () => {
  const f = fixture(); f.args.github.rest.actions.listWorkflowRunArtifacts = async () => ({data: {artifacts: []}});
  await select(f.args); assert.deepEqual(f.outputs, {});
});

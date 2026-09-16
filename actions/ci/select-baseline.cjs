'use strict';

// Only successful prior push runs of this exact workflow may provide raw inputs.
module.exports = async function selectBaseline({github, context, core, env = process.env}) {
  const {owner, repo} = context.repo;
  const event = context.eventName;
  if (event !== 'push' && event !== 'pull_request' && event !== 'workflow_dispatch') {
    core.notice('Baseline lookup supports push, pull_request and workflow_dispatch events only.');
    return;
  }
  if (event === 'pull_request' && context.payload.pull_request?.head?.repo?.full_name !== `${owner}/${repo}`) {
    core.notice('Fork pull requests do not receive baseline input artifacts.');
    return;
  }
  const branch = context.payload.repository?.default_branch;
  if (!branch) throw new Error('Default branch metadata is unavailable');
  const current = (await github.rest.actions.getWorkflowRun({owner, repo, run_id: context.runId})).data;
  const currentTime = Date.parse(current.created_at);
  const retention = Number(env.RETENTION);
  if (!Number.isFinite(currentTime) || !Number.isInteger(retention) || retention < 1 || retention > 30) {
    throw new Error('Invalid baseline lookup bounds');
  }
  const cutoff = Date.now() - retention * 86400000;
  // Bound API work; artifact availability is an optimization, not a proof assumption.
  for (let page = 1; page <= 3; page++) {
    const response = await github.rest.actions.listWorkflowRuns({
      owner, repo, workflow_id: current.workflow_id, branch, event: 'push',
      status: 'success', per_page: 100, page,
    });
    const runs = response.data.workflow_runs;
    for (const run of runs) {
      const created = Date.parse(run.created_at);
      if (run.id === context.runId || !Number.isFinite(created) || created >= currentTime || created < cutoff ||
          run.workflow_id !== current.workflow_id || run.event !== 'push' ||
          run.conclusion !== 'success' || run.head_branch !== branch ||
          run.head_repository?.full_name !== `${owner}/${repo}`) continue;
      const artifacts = (await github.rest.actions.listWorkflowRunArtifacts({
        owner, repo, run_id: run.id, name: env.ARTIFACT_NAME, per_page: 100,
      })).data.artifacts;
      const artifact = artifacts.find(item => item.name === env.ARTIFACT_NAME && !item.expired &&
        item.size_in_bytes > 0 && item.size_in_bytes <= 25 * 1024 * 1024 &&
        item.workflow_run?.id === run.id && item.workflow_run?.head_sha === run.head_sha);
      if (!artifact) continue;
      core.setOutput('run-id', String(run.id));
      core.setOutput('sha', run.head_sha);
      core.setOutput('artifact-id', String(artifact.id));
      return;
    }
    if (runs.length < 100) break;
  }
  core.notice('No recent compatible successful baseline found.');
};

'use strict';
const fs = require('node:fs');

module.exports = async function publishComment({github, context, core, env = process.env, readFile = fs.readFileSync}) {
  const {owner, repo} = context.repo;
  const eventPR = context.payload.pull_request;
  if (context.eventName !== 'pull_request' || eventPR?.head?.repo?.full_name !== `${owner}/${repo}`) return;
  if (!/^[a-f0-9]{32}$/.test(env.COMMENT_KEY || '')) throw new Error('Invalid comment key');
  const issue_number = eventPR.number;
  const current = (await github.rest.pulls.get({owner, repo, pull_number: issue_number})).data;
  if (current.state !== 'open' || current.head.sha !== eventPR.head.sha ||
      current.head.repo?.full_name !== `${owner}/${repo}`) {
    core.notice('Skipped stale WorldBisect result: the PR head changed or closed.');
    return;
  }
  const marker = `<!-- worldbisect-ci:${env.COMMENT_KEY} -->`;
  let summary = readFile(env.SUMMARY_PATH, 'utf8');
  if (summary.length > 7000) summary = summary.slice(0, 7000) + '\n\nSummary truncated; see the diagnostic artifact.';
  const url = env.ARTIFACT_URL || '';
  const allowedPrefix = `${context.serverUrl || 'https://github.com'}/${owner}/${repo}/actions/runs/`;
  const artifact = url.startsWith(allowedPrefix) && /^\d+\/artifacts\/\d+$/.test(url.slice(allowedPrefix.length))
    ? `\n\n[Full diagnostic artifact](${url})` : '';
  const body = `${marker}\n${summary.trim()}${artifact}\n\nCommit: \`${current.head.sha}\``;
  const comments = await github.paginate(github.rest.issues.listComments, {owner, repo, issue_number, per_page: 100});
  const existing = comments.find(item => item.user?.login === 'github-actions[bot]' &&
    item.user?.type === 'Bot' && item.body?.startsWith(marker + '\n'));
  // Recheck immediately before publication to avoid overwriting a newer head's result.
  const latest = (await github.rest.pulls.get({owner, repo, pull_number: issue_number})).data;
  if (latest.state !== 'open' || latest.head.sha !== current.head.sha) return;
  if (existing) await github.rest.issues.updateComment({owner, repo, comment_id: existing.id, body});
  else await github.rest.issues.createComment({owner, repo, issue_number, body});
};

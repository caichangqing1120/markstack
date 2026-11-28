const { localOnlyErrors } = require('./repository-policy.cjs');

async function checkPullRequest({ github, context }) {
  const { data: pull } = await github.rest.pulls.get({ ...context.repo, pull_number: context.payload.pull_request.number });
  const files = await github.paginate(github.rest.pulls.listFiles, { ...context.repo, pull_number: pull.number, per_page: 100 });
  const errors = localOnlyErrors(files);
  if (['OWNER', 'MEMBER', 'COLLABORATOR'].includes(pull.author_association)) {
    return errors;
  }
  if (pull.user.type === 'Bot' && pull.user.login === 'dependabot[bot]') {
    if (files.length && files.every(file => /^(?:go\.(?:mod|sum)|\.github\/workflows\/[^/]+\.ya?ml)$/.test(file.filename))) return errors;
  }
  const body = pull.body || '';
  const issueMatch = body.match(/^\s*(?:Closes|Fixes|Resolves|Issue|Refs):?\s+#([1-9]\d*)\s*$/im);
  if (!issueMatch) {
    errors.push('请单独一行填写 Closes #123 或 Refs #123，关联本仓库已认领的 Issue。');
  } else {
    const { data: issue } = await github.rest.issues.get({ ...context.repo, issue_number: Number(issueMatch[1]) });
    if (issue.pull_request || issue.state !== 'open' || !issue.assignees.some(user => user.login.toLowerCase() === pull.user.login.toLowerCase())) {
      errors.push('关联项必须是开放 Issue，且 PR 作者须已通过 /claim 成为负责人。');
    }
  }
  return errors;
}

module.exports = { checkPullRequest };

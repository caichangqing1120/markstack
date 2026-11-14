function detectCommand(body) {
  if (/^\s*\/unclaim(?:ed)?\s*$/.test(body)) return 'unclaim';
  if (/^\s*\/close\s*$/.test(body)) return 'close';
  if (/(?:^|\s)\/claim(?:\s|$)/.test(body)) return 'claim';
  return '';
}

function ownsIssue(issue, login) {
  return issue.assignees.some(user => user.login.toLowerCase() === login.toLowerCase());
}

async function handleIssueCommand({ github, context }) {
  const { issue: eventIssue, comment } = context.payload;
  if (!eventIssue || eventIssue.pull_request || !comment || comment.user.type === 'Bot') return { applied: false };
  const command = detectCommand(comment.body);
  if (!command) return { applied: false };
  const login = comment.user.login;
  const options = { ...context.repo, issue_number: eventIssue.number };
  const reply = body => github.rest.issues.createComment({ ...options, body });
  const { data: issue } = await github.rest.issues.get(options);
  if (issue.state !== 'open') {
    await reply('此 Issue 已关闭，未改变认领或状态。');
    return { applied: false };
  }

  if (command === 'claim') {
    if (issue.assignees.length) {
      await reply(ownsIssue(issue, login)
        ? `@${login} 已认领此 Issue。请先确认需求、验收标准和 Spec/计划，再开始实现。`
        : `此 Issue 已由 ${issue.assignees.map(user => `@${user.login}`).join(', ')} 认领，未改变负责人。`);
      return { applied: false };
    }
    try {
      await github.rest.issues.addAssignees({ ...options, assignees: [login] });
    } catch (error) {
      if (error.status !== 422) throw error;
      await reply(`@${login} 认领失败：GitHub 不允许此次分配。请联系维护者；未报告认领成功。`);
      return { applied: false };
    }
    const { data: assigned } = await github.rest.issues.get(options);
    if (!ownsIssue(assigned, login)) {
      await reply(`@${login} 的分配未确认，不能视为认领成功，请联系维护者。`);
      return { applied: false };
    }
    await reply(`@${login} 认领成功。请先确认范围与验收条件，Spec、计划及原始证据仅保存在本地；PR 关联本 Issue，不提交或上传文档、截图与原始日志。测试通过不替代原生输入和视觉验收。`);
    return { applied: true };
  }

  if (!ownsIssue(issue, login)) {
    await reply(`@${login} 不能${command === 'close' ? '关闭' : '释放'}此 Issue：只有当前负责人可以执行 /${command}。`);
    return { applied: false };
  }
  if (command === 'unclaim') {
    await github.rest.issues.removeAssignees({ ...options, assignees: [login] });
    const { data: released } = await github.rest.issues.get(options);
    if (ownsIssue(released, login)) throw new Error('GitHub did not remove the assignee');
    await reply(`@${login} 已释放认领，Issue 保持开放。`);
    return { applied: true };
  }

  const { data: repository } = await github.rest.repos.get(context.repo);
  const fullName = `${context.repo.owner}/${context.repo.repo}`;
  const events = await github.paginate(github.rest.issues.listEventsForTimeline, { ...options, per_page: 100 });
  const seen = new Set();
  for (const event of events) {
    if (event.event !== 'cross-referenced') continue;
    const url = event.source?.issue?.pull_request?.url;
    if (!url) continue;
    const parsed = new URL(url);
    const prefix = `/repos/${fullName}/pulls/`;
    if (parsed.origin !== 'https://api.github.com' || !parsed.pathname.toLowerCase().startsWith(prefix.toLowerCase())) continue;
    const suffix = parsed.pathname.slice(prefix.length);
    if (!/^[1-9]\d*$/.test(suffix) || seen.has(suffix)) continue;
    seen.add(suffix);
    const { data: pull } = await github.rest.pulls.get({ ...context.repo, pull_number: Number(suffix) });
    if (!pull.merged_at || pull.user?.login.toLowerCase() !== login.toLowerCase() ||
        pull.base.ref !== repository.default_branch || pull.base.repo.full_name.toLowerCase() !== fullName.toLowerCase()) continue;
    const { data: current } = await github.rest.issues.get(options);
    if (current.state !== 'open' || !ownsIssue(current, login)) {
      await reply('Issue 状态或负责人已变化，未关闭。');
      return { applied: false };
    }
    await github.rest.issues.update({ ...options, state: 'closed', state_reason: 'completed' });
    await reply(`@${login} 的关联 PR #${pull.number} 已合入 ${repository.default_branch}，Issue 已关闭。此动作不表示尚未记录的业务或视觉验收通过。`);
    return { applied: true };
  }
  await reply(`@${login} 不能关闭：尚无由你提交、关联本 Issue 并合入本仓库 ${repository.default_branch} 的 PR。`);
  return { applied: false };
}

module.exports = { detectCommand, handleIssueCommand };

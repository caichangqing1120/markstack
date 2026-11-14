const test = require('node:test');
const assert = require('node:assert/strict');
const { detectCommand, handleIssueCommand } = require('./issue-commands.cjs');
const { fixture, linkedPull } = require('./test-fixture.cjs');

test('commands follow Motty wording without accepting destructive trailing text', () => {
  for (const [body, expected] of [
    ['/claim', 'claim'], ['I would like to /claim this', 'claim'],
    [' /unclaim\n', 'unclaim'], ['/unclaimed', 'unclaim'], ['/close', 'close'],
    ['/claiming', ''], ['/close please', ''], ['/unclaim later', ''], ['hello', ''],
  ]) assert.equal(detectCommand(body), expected, body);
});

test('claim records the real assignee and confirms ownership', async () => {
  const setup = fixture();
  await handleIssueCommand(setup);
  assert.deepEqual(setup.state.issue.assignees, [{ login: 'alice' }]);
  assert.match(setup.state.comments.at(-1), /@alice/);
});

test('closed issues and existing owners cannot be taken over', async () => {
  for (const issue of [{ state: 'closed' }, { assignees: [{ login: 'bob' }] }]) {
    const setup = fixture({ issue });
    const original = structuredClone(setup.state.issue);
    await handleIssueCommand(setup);
    assert.deepEqual(setup.state.issue, original);
    assert.equal(setup.state.comments.length, 1);
  }
});

test('failed or silently ignored assignments never report a successful claim', async () => {
  for (const overrides of [
    { assignmentError: Object.assign(new Error('not assignable'), { status: 422 }) },
    { ignoreAssignment: true },
  ]) {
    const setup = fixture(overrides);
    const result = await handleIssueCommand(setup);
    assert.equal(result.applied, false);
    assert.deepEqual(setup.state.issue.assignees, []);
    assert.match(setup.state.comments.at(-1), /失败|未确认/);
  }
});

test('only the current assignee can release an issue', async () => {
  const setup = fixture({ issue: { assignees: [{ login: 'bob' }] } });
  setup.context.payload.comment.body = '/unclaim';
  await handleIssueCommand(setup);
  assert.deepEqual(setup.state.issue.assignees, [{ login: 'bob' }]);
  setup.context.payload.comment.user.login = 'bob';
  await handleIssueCommand(setup);
  assert.deepEqual(setup.state.issue.assignees, []);
  assert.equal(setup.state.issue.state, 'open');
});

test('bot comments and pull-request comments have no issue side effects', async () => {
  for (const variant of ['bot', 'pull']) {
    const setup = fixture();
    if (variant === 'bot') setup.context.payload.comment.user.type = 'Bot';
    else setup.context.payload.issue.pull_request = { url: 'https://api.github.com/repos/owner/markstack/pulls/7' };
    await handleIssueCommand(setup);
    assert.deepEqual(setup.state.issue.assignees, []);
    assert.deepEqual(setup.state.comments, []);
  }
});

test('close discovers a same-author merged PR on a later timeline page', async () => {
  const setup = fixture({
    issue: { assignees: [{ login: 'alice' }] },
    pages: [[{ event: 'commented' }], [linkedPull()]],
    pull: { merged_at: '2026-11-10T03:00:00Z' },
  });
  setup.context.payload.comment.body = '/close';
  await handleIssueCommand(setup);
  assert.equal(setup.state.issue.state, 'closed');
  assert.equal(setup.state.issue.state_reason, 'completed');
  assert.match(setup.state.comments.at(-1), /#11/);
});

test('close refuses unmerged, wrong-author, wrong-base and foreign-repository PRs', async () => {
  for (const overrides of [
    { pull: { merged_at: null } },
    { pull: { merged_at: '2026-11-10T03:00:00Z', user: { login: 'bob' } } },
    { pull: { merged_at: '2026-11-10T03:00:00Z', base: { ref: 'other', repo: { full_name: 'owner/markstack' } } } },
    { pages: [[linkedPull(11, 'elsewhere/markstack')]], pull: { merged_at: '2026-11-10T03:00:00Z' } },
  ]) {
    const setup = fixture({ issue: { assignees: [{ login: 'alice' }] }, pages: [[linkedPull()]], ...overrides });
    setup.context.payload.comment.body = '/close';
    await handleIssueCommand(setup);
    assert.equal(setup.state.issue.state, 'open');
    assert.match(setup.state.comments.at(-1), /不能关闭/);
  }
});

test('close refuses a commenter who does not own the issue', async () => {
  const setup = fixture({ issue: { assignees: [{ login: 'bob' }] }, pages: [[linkedPull()]], pull: { merged_at: '2026-11-10T03:00:00Z' } });
  setup.context.payload.comment.body = '/close';
  await handleIssueCommand(setup);
  assert.equal(setup.state.issue.state, 'open');
});

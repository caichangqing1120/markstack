const test = require('node:test');
const assert = require('node:assert/strict');
const { checkPullRequest } = require('./pr-policy.cjs');
const { fixture } = require('./test-fixture.cjs');

test('a source-only PR with an owned issue passes without publishing local Spec or plan', async () => {
  const setup = fixture({ issue: { assignees: [{ login: 'alice' }] }, pull: { body: 'Refs #7\n\nImplement the agreed issue scope.' } });
  setup.github.rest.repos.getContent = async () => { throw new Error('Private planning files must not be fetched'); };
  assert.deepEqual(await checkPullRequest(setup), []);
});

test('a PR cannot skip the issue or claim another contributor issue', async () => {
  for (const overrides of [{ pull: { body: '' } }, { issue: { assignees: [{ login: 'bob' }] } }]) {
    const setup = fixture(overrides);
    assert.ok((await checkPullRequest(setup)).length > 0);
  }
});

test('local documents, plans and screenshots are rejected before any bot exemption', async () => {
  for (const filename of ['docs/specs/private.md', 'screenshots/current.png', 'review/plan-next.md', 'review/plans/next.txt', 'captures/window.png', 'assets/screenshots/current.png', 'assets/Screenshot-2026.png', 'review/acceptance.md', 'review/execution-ledger.json', 'assets/native-capture/current.png', 'assets/capture.png', 'local/progress.json', 'local/spec.md', 'evidence/source.sha256']) {
    for (const bot of [false, true]) {
      const setup = fixture({
        issue: { assignees: [{ login: 'alice' }] },
        pull: bot ? { user: { login: 'dependabot[bot]', type: 'Bot' }, body: '' } : {},
        changedFiles: [{ filename, status: 'added' }],
      });
      assert.ok((await checkPullRequest(setup)).some(error => error.includes(filename)), filename);
    }
  }
});

test('removing local-only files and keeping real product resources is allowed', async () => {
  const setup = fixture({
    issue: { assignees: [{ login: 'alice' }] },
    changedFiles: [{ filename: 'docs/private.md', status: 'removed' }, { filename: 'assets/markstack-icon.png', status: 'modified' }, { filename: 'assets/lucide/search.svg', status: 'added' }],
  });
  assert.deepEqual(await checkPullRequest(setup), []);
});

test('renaming a file into a local-only directory is rejected', async () => {
  const setup = fixture({ issue: { assignees: [{ login: 'alice' }] }, changedFiles: [{ filename: 'docs/secret.md', previous_filename: 'README.md', status: 'renamed' }] });
  assert.ok((await checkPullRequest(setup)).some(error => error.includes('docs/secret.md')));
});

test('an already closed issue is not an active development claim', async () => {
  const setup = fixture({ issue: { state: 'closed', assignees: [{ login: 'alice' }] } });
  assert.ok((await checkPullRequest(setup)).length > 0);
});

test('dependency-only Dependabot PRs can pass without a human claim', async () => {
  const setup = fixture({
    pull: { user: { login: 'dependabot[bot]', type: 'Bot' }, body: '' },
    changedFiles: [{ filename: 'go.mod' }, { filename: 'go.sum' }],
  });
  assert.deepEqual(await checkPullRequest(setup), []);
});

test('Dependabot identity does not exempt arbitrary application changes', async () => {
  const setup = fixture({
    pull: { user: { login: 'dependabot[bot]', type: 'Bot' }, body: '' },
    changedFiles: [{ filename: 'cmd/markstack/main.go' }],
  });
  assert.ok((await checkPullRequest(setup)).length > 0);
});

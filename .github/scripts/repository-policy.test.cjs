const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync, spawnSync } = require('node:child_process');

test('tracked content gate blocks force-added private materials and permits product assets', context => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'markstack-source-only-'));
  context.after(() => fs.rmSync(temporary, { recursive: true, force: true }));
  execFileSync('git', ['init', '-q', temporary]);
  const files = ['README.md', 'assets/markstack-icon.png', 'cmd/markstack/main.go', 'docs/private.md', 'review/plan-next.md', 'captures/screen.png'];
  for (const filename of files) {
    fs.mkdirSync(path.dirname(path.join(temporary, filename)), { recursive: true });
    fs.writeFileSync(path.join(temporary, filename), 'fixture');
  }
  execFileSync('git', ['add', '.'], { cwd: temporary });
  const script = path.resolve(__dirname, 'repository-policy.cjs');
  const rejected = spawnSync(process.execPath, [script], { cwd: temporary, encoding: 'utf8' });
  assert.equal(rejected.status, 1, rejected.stderr);
  for (const filename of files.slice(3)) assert.ok(rejected.stderr.includes(filename), rejected.stderr);
  execFileSync('git', ['rm', '--cached', '--', ...files.slice(3)], { cwd: temporary });
  const accepted = spawnSync(process.execPath, [script], { cwd: temporary, encoding: 'utf8' });
  assert.equal(accepted.status, 0, accepted.stderr);
  assert.match(accepted.stdout, /source-only/);
});

test('contribution entrypoints do not require private documents or upload screenshots', () => {
  const root = path.resolve(__dirname, '../..');
  const contributing = fs.readFileSync(path.join(root, 'CONTRIBUTING.md'), 'utf8');
  const template = fs.readFileSync(path.join(root, '.github/pull_request_template.md'), 'utf8');
  assert.match(contributing, /仅.*本地|只.*本地/);
  assert.doesNotMatch(template, /^Spec:\s*docs\//m);
  assert.doesNotMatch(template, /^Plan:\s*docs\//m);
  assert.match(template, /不.*docs|未.*docs/);
  const workflow = fs.readFileSync(path.join(root, '.github/workflows/ci.yml'), 'utf8');
  assert.match(workflow, /node \.github\/scripts\/repository-policy\.cjs/);
  for (const filename of ['feature.yml', 'bug.yml']) {
    const issueTemplate = fs.readFileSync(path.join(root, '.github/ISSUE_TEMPLATE', filename), 'utf8');
    assert.match(issueTemplate, /不上传.*截图|不附.*截图/);
    assert.doesNotMatch(issueTemplate, /复用 docs\/specs/);
  }
});

test('public acceptance does not depend on private docs or historical screenshots', () => {
  const root = path.resolve(__dirname, '../..');
  const source = fs.readFileSync(path.join(root, 'scripts/acceptance.sh'), 'utf8');
  assert.doesNotMatch(source, /\$root\/docs\//);
  assert.doesNotMatch(source, /compare-workbench-screenshot\.sh/);
  assert.match(source, /check-ui-screenshot\.cjs/);
  for (const requirement of ['Wrapped-Selection.md', 'heading-native-bounds.log', 'line-edge-tests.log', 'cross-style-tests.log', 'aligned-cell-protocol.log']) assert.ok(source.includes(requirement));
  const comparisonTest = fs.readFileSync(path.join(root, 'cmd/markstack/native_visual_compare_test.go'), 'utf8');
  assert.doesNotMatch(comparisonTest, /docs\/screenshots\//);
});

test('claim replies keep planning files and screenshots local', async () => {
  const { handleIssueCommand } = require('./issue-commands.cjs');
  const { fixture } = require('./test-fixture.cjs');
  const setup = fixture();
  await handleIssueCommand(setup);
  assert.equal(setup.state.comments.length, 1);
  assert.match(setup.state.comments[0], /本地/);
  assert.match(setup.state.comments[0], /不.*截图/);
  assert.doesNotMatch(setup.state.comments[0], /在 docs\/specs/);
});

test('plain text acceptance does not click a disabled reading control', () => {
  const source = fs.readFileSync(path.resolve(__dirname, '../../scripts/acceptance.sh'), 'utf8');
  const start = source.indexOf('click "literal.txt"');
  const end = source.indexOf('click "page.html"', start);
  assert.ok(start >= 0 && end > start);
  const scenario = source.slice(start, end);
  assert.doesNotMatch(scenario, /click "阅读"/);
  assert.match(scenario, /key cmd\+3/);
  assert.match(scenario, /plain-stays-source-740\.png/);
});

test('menu layout acceptance follows the screenshot command that settles transitions', () => {
  const source = fs.readFileSync(path.resolve(__dirname, '../../scripts/acceptance.sh'), 'utf8');
  assert.doesNotMatch(source, /click "文件"\nlayout\nshot /);
  for (const filename of ['pdf-menu.png', 'file-menu.png', 'template-menu-740.png', 'rich-html-new-menu-740.png']) {
    assert.ok(source.includes(`shot "$artifacts/${filename}"\nlayout`), filename);
  }
});

test('acceptance identity includes product assets and licenses', () => {
  const root = path.resolve(__dirname, '../..');
  const source = fs.readFileSync(path.join(root, 'scripts/acceptance.sh'), 'utf8');
  const inventory = source.match(/^rg --files ([^|\n]+) \| LC_ALL=C sort \| xargs shasum -a 256 > "\$artifacts\/source-manifest\.txt"$/m);
  assert.ok(inventory, 'Missing source inventory');
  assert.ok(inventory[1].trim().split(/\s+/).includes('assets'), 'Product resources are not bound to the candidate');
  const metadata = source.match(/^shasum -a 256 ([^>\n]+) >> "\$artifacts\/source-manifest\.txt"$/m);
  assert.ok(metadata, 'Missing candidate metadata');
  for (const filename of metadata[1].trim().split(/\s+/)) {
    assert.ok(fs.existsSync(path.join(root, filename)), `Missing metadata input: ${filename}`);
    assert.ok(fs.statSync(path.join(root, filename)).isFile(), filename);
  }
  for (const filename of ['go.mod', 'go.sum', 'viva.toml']) {
    assert.ok(metadata[1].trim().split(/\s+/).includes(filename), filename);
  }
  for (const filename of ['assets/markstack-icon.png', 'assets/lucide/LICENSE', 'internal/exportpdf/font/OFL.txt', 'internal/exportpdf/font/NotoSansSC-Regular.ttf.gz']) {
    assert.ok(fs.existsSync(path.join(root, filename)), filename);
    assert.ok(inventory[1].trim().split(/\s+/).some(directory => filename.startsWith(`${directory}/`)), filename);
  }
});

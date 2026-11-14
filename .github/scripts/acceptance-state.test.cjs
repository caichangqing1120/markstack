const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

const scripts = path.resolve(__dirname, '../../scripts');
const stateVariables = ['SETTINGS', 'RECOVERY', 'SESSION', 'RECENT'].map(name => `MARKSTACK_${name}_PATH`);

test('acceptance isolates tests and headless scenarios from inherited application state', context => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'markstack-acceptance-state-'));
  context.after(() => fs.rmSync(temporary, { recursive: true, force: true }));
  const root = path.join(temporary, 'repository with spaces');
  const bin = path.join(temporary, 'bin');
  const capture = path.join(temporary, 'processes.jsonl');
  for (const directory of ['scripts', 'cmd/markstack', 'internal', 'examples', 'build']) {
    fs.mkdirSync(path.join(root, directory), { recursive: true });
  }
  fs.mkdirSync(bin);
  fs.copyFileSync(path.join(scripts, 'acceptance.sh'), path.join(root, 'scripts/acceptance.sh'));
  if (fs.existsSync(path.join(scripts, 'acceptance-state.sh'))) {
    fs.copyFileSync(path.join(scripts, 'acceptance-state.sh'), path.join(root, 'scripts/acceptance-state.sh'));
  }
  const recorder = `
const fs = require('node:fs');
const path = require('node:path');
const variables = ${JSON.stringify(stateVariables)};
function record(kind) {
  const paths = Object.fromEntries(variables.map(name => [name, process.env[name]]));
  const recovery = paths.MARKSTACK_RECOVERY_PATH;
  fs.appendFileSync(process.env.ACCEPTANCE_TEST_CAPTURE, JSON.stringify({ kind, args: process.argv.slice(2), paths, hasRecovery: Boolean(recovery && fs.existsSync(recovery)) }) + '\\n');
}
`;
  const application = `#!${process.execPath}\n${recorder}\nrecord('headless');\nprocess.exit(71);\n`;
  const fakeGo = `#!${process.execPath}
${recorder}
record('go');
const args = process.argv.slice(2);
if (args[0] === 'test') {
  const recovery = process.env.MARKSTACK_RECOVERY_PATH;
  if (recovery) {
    fs.mkdirSync(path.dirname(recovery), { recursive: true });
    fs.writeFileSync(recovery, 'test-owned dirty draft');
  }
  const artifacts = process.env.MARKSTACK_EXPORT_ARTIFACT_DIR;
  if (artifacts) {
    fs.mkdirSync(artifacts, { recursive: true });
    fs.writeFileSync(path.join(artifacts, 'fixture.html'), 'fixture');
  }
}
if (args[0] === 'build') {
  const output = args[args.indexOf('-o') + 1];
  fs.mkdirSync(path.dirname(output), { recursive: true });
  fs.writeFileSync(output, ${JSON.stringify(application)}, { mode: 0o700 });
}
`;
  fs.writeFileSync(path.join(bin, 'go'), fakeGo, { mode: 0o700 });
  for (const command of ['gofmt', 'pdftoppm']) {
    fs.writeFileSync(path.join(bin, command), '#!/bin/sh\nexit 0\n', { mode: 0o700 });
  }
  const inherited = {};
  for (const variable of stateVariables) {
    const filename = path.join(temporary, `${variable}.json`);
    fs.writeFileSync(filename, 'caller-owned state', { mode: 0o600 });
    inherited[variable] = filename;
  }
  const environment = { ...process.env, ...inherited, PATH: `${bin}${path.delimiter}${process.env.PATH}`, ACCEPTANCE_TEST_CAPTURE: capture };
  delete environment.MARKSTACK_EXPORT_ARTIFACT_DIR;
  delete environment.MARKSTACK_PDF_ARTIFACT_DIR;
  const result = spawnSync('/bin/bash', [path.join(root, 'scripts/acceptance.sh')], { cwd: root, env: environment, encoding: 'utf8' });
  assert.equal(result.status, 71, result.stderr);
  const processes = fs.readFileSync(capture, 'utf8').trim().split('\n').map(JSON.parse);
  const headless = processes.find(process => process.kind === 'headless');
  assert.ok(headless, 'the actual acceptance script must reach its first headless scenario');
  assert.equal(headless.hasRecovery, false, 'headless scenario inherited a previous dirty draft');
  const isolated = processes.filter(process => process.kind === 'headless' || process.args[0] === 'test');
  const directories = new Set();
  for (const process of isolated) {
    const parents = new Set();
    for (const variable of stateVariables) {
      assert.ok(process.paths[variable], `${process.kind} has no private ${variable}`);
      assert.notEqual(process.paths[variable], inherited[variable]);
      parents.add(path.dirname(process.paths[variable]));
    }
    assert.equal(parents.size, 1, 'a default process uses one private state directory');
    const directory = [...parents][0];
    assert.ok(!directories.has(directory), 'independent processes shared their state directory');
    directories.add(directory);
  }
  for (const filename of Object.values(inherited)) {
    assert.equal(fs.readFileSync(filename, 'utf8'), 'caller-owned state');
  }
});

test('acceptance preserves explicit state paths for intentional cross-process scenarios', context => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'markstack-acceptance-shared-'));
  context.after(() => fs.rmSync(temporary, { recursive: true, force: true }));
  const shared = path.join(temporary, 'shared');
  const probe = `
const fs = require('node:fs');
const path = require('node:path');
const variables = ${JSON.stringify(stateVariables)};
const paths = variables.map(name => process.env[name]);
console.log(JSON.stringify({ paths, existing: paths.map(filename => fs.existsSync(filename)) }));
for (const filename of paths) {
  fs.mkdirSync(path.dirname(filename), { recursive: true, mode: 0o700 });
  fs.writeFileSync(filename, 'intentional shared state');
}
`;
  const command = `
set -euo pipefail
work="$1"
source "$2"
for stage in first second; do
  MARKSTACK_SETTINGS_PATH="$5/settings.json" \\
  MARKSTACK_RECOVERY_PATH="$5/recovery.json" \\
  MARKSTACK_SESSION_PATH="$5/session.json" \\
  MARKSTACK_RECENT_PATH="$5/recent.json" \\
  acceptance_run "$3" -e "$4"
done
`;
  const result = spawnSync('/bin/bash', ['-c', command, 'acceptance-shared-test', temporary, path.join(scripts, 'acceptance-state.sh'), process.execPath, probe, shared], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  const [first, second] = result.stdout.trim().split('\n').map(JSON.parse);
  assert.deepEqual(first.paths, second.paths);
  assert.deepEqual(first.existing, [false, false, false, false]);
  assert.deepEqual(second.existing, [true, true, true, true]);
  for (const filename of second.paths) {
    assert.equal(path.dirname(filename), shared);
    assert.equal(fs.readFileSync(filename, 'utf8'), 'intentional shared state');
  }
});

test('acceptance routes every application and Go test launch through private state', () => {
  const source = fs.readFileSync(path.join(scripts, 'acceptance.sh'), 'utf8');
  const applications = source.split('\n').filter(line => line.includes('./build/markstack-headless'));
  const suites = source.split('\n').filter(line => /\bgo test\b/.test(line));
  assert.ok(applications.length > 0);
  assert.ok(suites.length > 0);
  for (const line of applications) assert.match(line, /acceptance_run \.\/build\/markstack-headless/);
  for (const line of suites) assert.match(line, /acceptance_run go test/);
});

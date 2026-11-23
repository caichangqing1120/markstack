const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

const repository = path.resolve(__dirname, '../..');
const available = process.platform === 'linux' && spawnSync('dpkg-deb', ['--version']).status === 0;

test('Linux developer package preserves its payload and includes both asset notices', { skip: !available }, context => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'markstack-deb-'));
  context.after(() => fs.rmSync(temporary, { recursive: true, force: true }));
  const root = path.join(temporary, 'source with spaces');
  const stock = path.join(temporary, 'stock');
  const bin = path.join(temporary, 'bin');
  const font = 'internal/exportpdf/font/OFL.txt';
  const icon = 'assets/lucide/LICENSE';
  for (const directory of ['scripts', path.dirname(font), path.dirname(icon)]) {
    fs.mkdirSync(path.join(root, directory), { recursive: true });
  }
  for (const file of [font, icon, 'scripts/package-linux.sh']) {
    fs.copyFileSync(path.join(repository, file), path.join(root, file));
  }
  fs.mkdirSync(bin);
  for (const directory of ['DEBIAN', 'usr/bin']) fs.mkdirSync(path.join(stock, directory), { recursive: true });
  fs.writeFileSync(path.join(stock, 'DEBIAN/control'), [
    'Package: markstack', 'Version: 0.1.0', 'Architecture: arm64', 'Maintainer: MarkStack',
    'Installed-Size: 1', 'Depends: libxcb1', 'Description: test fixture', '',
  ].join('\n'));
  fs.writeFileSync(path.join(stock, 'usr/bin/markstack'), 'test program\n', { mode: 0o755 });
  const sum = spawnSync('md5sum', ['usr/bin/markstack'], { cwd: stock });
  assert.equal(sum.status, 0);
  fs.writeFileSync(path.join(stock, 'DEBIAN/md5sums'), sum.stdout);
  const packageName = 'markstack_0.1.0_arm64.deb';
  const stockPackage = path.join(temporary, packageName);
  const epoch = { ...process.env, SOURCE_DATE_EPOCH: '1700000000' };
  assert.equal(spawnSync('dpkg-deb', ['--root-owner-group', '--build', stock, stockPackage], { env: epoch }).status, 0);
  fs.writeFileSync(path.join(bin, 'go'), `#!/bin/bash
set -euo pipefail
[[ "$*" == 'tool viva package --target linux --arch arm64 --format deb --no-sign --out '* ]]
[[ \${FAIL_PACKAGE:-} != 1 ]] || exit 27
mkdir -p -- "\${@: -1}"
cp -- "$STOCK_PACKAGE" "\${@: -1}/"
`, { mode: 0o700 });
  const env = { ...epoch, PATH: `${bin}${path.delimiter}${process.env.PATH}`, STOCK_PACKAGE: stockPackage };
  const run = (options = {}) => spawnSync('/bin/bash', [path.join(root, 'scripts/package-linux.sh'), '--no-sign', 'arm64'],
    { encoding: 'utf8', env: { ...env, ...options } });
  const first = run();
  assert.equal(first.status, 0, first.stderr);
  const output = path.join(root, 'build', packageName);
  const previous = fs.readFileSync(output);
  const second = run();
  assert.equal(second.status, 0, second.stderr);
  assert.deepEqual(fs.readFileSync(output), previous);
  const failed = run({ FAIL_PACKAGE: '1' });
  assert.equal(failed.status, 27, failed.stderr);
  assert.deepEqual(fs.readFileSync(output), previous);
  assert.deepEqual(fs.readdirSync(path.dirname(output)), [packageName]);

  const extracted = path.join(temporary, 'extracted');
  assert.equal(spawnSync('dpkg-deb', ['-R', output, extracted]).status, 0);
  const read = file => fs.readFileSync(path.join(extracted, file));
  assert.deepEqual(read('usr/bin/markstack'), fs.readFileSync(path.join(stock, 'usr/bin/markstack')));
  assert.equal(fs.statSync(path.join(extracted, 'usr/bin/markstack')).mode & 0o777, 0o755);
  const notice = read('usr/share/doc/markstack/copyright');
  for (const file of [font, icon]) assert.ok(notice.includes(fs.readFileSync(path.join(root, file))));
  const control = read('DEBIAN/control').toString();
  assert.equal(control, fs.readFileSync(path.join(stock, 'DEBIAN/control'), 'utf8')
    .replace('Installed-Size: 1', `Installed-Size: ${1 + Math.ceil(notice.length / 1024)}`));
  assert.equal(spawnSync('md5sum', ['-c', 'DEBIAN/md5sums'], { cwd: extracted }).status, 0);
  assert.match(read('DEBIAN/md5sums').toString(), /  usr\/share\/doc\/markstack\/copyright\n/);
});

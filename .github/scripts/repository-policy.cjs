const { execFileSync } = require('node:child_process');

function isLocalOnlyPath(filename) {
  const normalized = filename.replaceAll('\\', '/');
  const segments = normalized.toLowerCase().split('/');
  const basename = segments.at(-1);
  if (segments.some(segment => ['docs', 'specs', 'plans', 'screenshots', '.superpowers', 'review', 'reviews', 'evidence', 'artifacts', 'captures', 'reports'].includes(segment))) return true;
  if (/^plan[^/]*\.md$/.test(basename)) return true;
  if (/^(?:acceptance|verification|progress|execution[-_]ledger|spec)(?:[-_.].*)?\.(?:md|jsonl?|txt|ya?ml|log)$/.test(basename)) return true;
  if (/\.(?:png|jpe?g|webp|gif|avif|bmp|tiff?|heic)$/.test(basename)) {
    return normalized !== 'assets/markstack-icon.png';
  }
  return false;
}

function localOnlyErrors(files) {
  return files.filter(file => file.status !== 'removed' && isLocalOnlyPath(file.filename))
    .map(file => `禁止提交本地文档、计划、账本或截图：${file.filename}。当前仅允许必要产品位图 assets/markstack-icon.png，新增产品位图须维护者审查后明确放行。`);
}

if (require.main === module) {
  const files = execFileSync('git', ['ls-files', '-z'], { encoding: 'utf8' }).split('\0').filter(Boolean).map(filename => ({ filename }));
  const errors = localOnlyErrors(files);
  if (errors.length) {
    console.error(errors.join('\n'));
    process.exitCode = 1;
  } else {
    console.log('Repository source-only content policy passed.');
  }
}

module.exports = { isLocalOnlyPath, localOnlyErrors };

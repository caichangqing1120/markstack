const assert = require('node:assert/strict');
const fs = require('node:fs');
const { execFileSync } = require('node:child_process');

function checkScreenshot(filename, width, height) {
  assert.ok(Number.isInteger(width) && Number.isInteger(height) && width >= 740 && width <= 4096 && height >= 520 && height <= 4096, 'Invalid expected screenshot size');
  assert.ok(fs.statSync(filename).isFile(), 'Missing screenshot');
  assert.ok(fs.readFileSync(filename).subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])), 'Screenshot must be lossless PNG');
  const metadata = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-select_streams', 'v:0', '-show_entries', 'stream=codec_name,width,height', '-of', 'json', filename], { encoding: 'utf8' }));
  assert.equal(metadata.streams.length, 1, 'Expected one screenshot stream');
  const stream = metadata.streams[0];
  assert.equal(stream.codec_name, 'png', 'Screenshot must be lossless PNG');
  assert.equal(stream.width, width, 'Screenshot width differs from the tested viewport');
  assert.equal(stream.height, height, 'Screenshot height differs from the tested viewport');
  const pixels = execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-i', filename, '-frames:v', '1', '-pix_fmt', 'rgba', '-f', 'rawvideo', 'pipe:1'], { maxBuffer: width * height * 4 + 1024 });
  assert.equal(pixels.length, width * height * 4, 'Incomplete screenshot pixels');
  let documentInk = 0;
  let documentBackground = 0;
  let chromeInk = 0;
  for (let row = 0; row < height; row++) {
    for (let column = 0; column < width; column++) {
      const offset = (row * width + column) * 4;
      const [red, green, blue, alpha] = pixels.subarray(offset, offset + 4);
      if (column >= 220 && column < width - 30 && row >= 110 && row < height - 26) {
        assert.equal(alpha, 255, 'Document screenshot is transparent');
        if (Math.max(red, green, blue) < 180) documentInk++;
        if (Math.min(red, green, blue) > 235) documentBackground++;
      }
      if (row < 32 && column >= 80 && column < width - 10 && Math.max(red, green, blue) < 180 && alpha === 255) chromeInk++;
    }
  }
  const documentPixels = (width - 250) * (height - 136);
  assert.ok(documentInk >= 100, 'Document screenshot has no visible content');
  assert.ok(documentBackground >= documentPixels / 10, 'Document screenshot has no readable light surface');
  assert.ok(chromeInk >= 50, 'Screenshot has no visible application controls');
  return { width, height, documentInk, documentBackground, chromeInk, goldenCompared: false };
}

if (require.main === module) {
  try {
    assert.equal(process.argv.length, 5, 'usage: node scripts/check-ui-screenshot.cjs CANDIDATE.png WIDTH HEIGHT');
    const result = checkScreenshot(process.argv[2], Number(process.argv[3]), Number(process.argv[4]));
    console.log(`Current UI PNG health checked: ${JSON.stringify(result)}; semantic/layout and native acceptance are separate.`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

module.exports = { checkScreenshot };

// Runs from the `vscode:prepublish` hook, which vsce invokes on every
// `vsce package` and `vsce publish`.
//
// It deliberately does NOT build the Go binary. scripts/package.js prunes bin/
// and builds exactly one binary for the target it is packaging; if this hook
// rebuilt every target afterwards, each VSIX would ship all of them again.
// Instead this compiles TypeScript and checks that a binary is in place.

const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');

const tsc = spawnSync('yarn', ['run', 'compile'], {
  stdio: 'inherit',
  cwd: root,
  shell: process.platform === 'win32',
});
if (tsc.status !== 0) {
  process.exit(tsc.status ?? 1);
}

const binDir = path.join(root, 'bin');
const candidates = ['hclschema-cli', 'hclschema-cli.exe'];
const present = candidates.filter((name) => fs.existsSync(path.join(binDir, name)));

if (present.length === 0) {
  console.error(
    '\nNo binary found in bin/.\n' +
      'Package through `yarn package` (this platform) or `yarn package:all` (every platform);\n' +
      'those build the right binary for each target. A bare `vsce package` cannot know which one to ship.\n',
  );
  process.exit(1);
}
if (present.length > 1) {
  console.error(
    `\nbin/ holds more than one binary (${present.join(', ')}).\n` +
      'A per-platform VSIX must carry exactly one. Run `yarn package` rather than packaging by hand.\n',
  );
  process.exit(1);
}

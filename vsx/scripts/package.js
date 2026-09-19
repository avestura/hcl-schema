// Produces one VSIX per platform.
//
// The extension used to ship every platform's binary in every VSIX, which made
// a ~38 MB download of which all but one binary was dead weight. Building a
// VSIX per target means each download carries exactly the binary it needs.

const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const TARGETS = [
  { vscode: 'win32-x64', goos: 'windows', goarch: 'amd64' },
  { vscode: 'win32-arm64', goos: 'windows', goarch: 'arm64' },
  { vscode: 'linux-x64', goos: 'linux', goarch: 'amd64' },
  { vscode: 'linux-arm64', goos: 'linux', goarch: 'arm64' },
  { vscode: 'darwin-x64', goos: 'darwin', goarch: 'amd64' },
  { vscode: 'darwin-arm64', goos: 'darwin', goarch: 'arm64' },
];

function run(cmd, args, opts) {
  console.log('> ' + [cmd].concat(args).join(' '));
  const r = spawnSync(cmd, args, Object.assign({ stdio: 'inherit', shell: process.platform === 'win32' }, opts));
  if (r.error) throw r.error;
  if (r.status !== 0) {
    throw new Error(`${cmd} exited with ${r.status}`);
  }
}

function hostVSCodeTarget() {
  const goos = process.platform === 'win32' ? 'win32' : process.platform;
  const arch = process.arch === 'x64' ? 'x64' : process.arch;
  return `${goos}-${arch}`;
}

function main() {
  const root = path.resolve(__dirname, '..');
  const all = process.argv.includes('--all');
  const binDir = path.join(root, 'bin');
  const distDir = path.join(root, 'dist');
  fs.mkdirSync(distDir, { recursive: true });

  const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));
  const wanted = all ? TARGETS : TARGETS.filter((t) => t.vscode === hostVSCodeTarget());
  if (wanted.length === 0) {
    throw new Error(`no packaging target matches this host (${hostVSCodeTarget()})`);
  }

  run('yarn', ['run', 'compile'], { cwd: root });

  for (const target of wanted) {
    // Each VSIX must contain one binary and no others, so bin/ is emptied
    // between targets rather than accumulating.
    fs.rmSync(binDir, { recursive: true, force: true });
    fs.mkdirSync(binDir, { recursive: true });

    run('node', [
      path.join('scripts', 'build-go-cli.js'),
      '--goos', target.goos,
      '--goarch', target.goarch,
    ], { cwd: root });

    const out = path.join('dist', `hcl-schema-${pkg.version}-${target.vscode}.vsix`);
    run('npx', ['--yes', '@vscode/vsce', 'package', '--target', target.vscode, '-o', out], { cwd: root });
    console.log(`packaged ${out}`);
  }
}

main();

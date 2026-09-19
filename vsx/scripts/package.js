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
  { vscode: 'linux-armhf', goos: 'linux', goarch: 'arm', goarm: '7' },
  { vscode: 'darwin-x64', goos: 'darwin', goarch: 'amd64' },
  { vscode: 'darwin-arm64', goos: 'darwin', goarch: 'arm64' },
  // Alpine is musl rather than glibc. The binary is built with CGO disabled,
  // so it is static and the same Linux build runs there — but VS Code treats
  // alpine as its own target, and with per-platform publishing there is no
  // universal package to fall back on. Leaving these out would mean Alpine
  // devcontainers get no extension at all.
  { vscode: 'alpine-x64', goos: 'linux', goarch: 'amd64' },
  { vscode: 'alpine-arm64', goos: 'linux', goarch: 'arm64' },
];

function run(cmd, args, opts) {
  console.log('> ' + [cmd].concat(args).join(' '));
  const r = spawnSync(cmd, args, Object.assign({ stdio: 'inherit', shell: process.platform === 'win32' }, opts));
  if (r.error) throw r.error;
  if (r.status !== 0) {
    throw new Error(`${cmd} exited with ${r.status}`);
  }
}

// vsce resolves to the pinned local install.
function vsce(root) {
  const bin = path.join(root, 'node_modules', '.bin', process.platform === 'win32' ? 'vsce.cmd' : 'vsce');
  if (!fs.existsSync(bin)) {
    throw new Error('@vscode/vsce is not installed. Run `yarn install` in vsx/ first.');
  }
  return bin;
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

    const buildArgs = [
      path.join('scripts', 'build-go-cli.js'),
      '--goos', target.goos,
      '--goarch', target.goarch,
    ];
    if (target.goarm) {
      buildArgs.push('--goarm', target.goarm);
    }
    run('node', buildArgs, { cwd: root });

    const out = path.join('dist', `hcl-schema-${pkg.version}-${target.vscode}.vsix`);
    // The pinned devDependency rather than npx, so a release always packages
    // with the vsce version recorded in yarn.lock.
    run(vsce(root), ['package', '--target', target.vscode, '-o', out], { cwd: root });
    console.log(`packaged ${out}`);
  }
}

main();

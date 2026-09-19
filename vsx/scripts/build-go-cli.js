// Builds the hclschema binary that the extension ships.
//
// With --goos/--goarch it writes a single binary straight into bin/, which is
// what per-platform packaging wants: only one binary belongs in a VSIX built
// for one platform. Without them it writes every target into bin/<goos>-<goarch>/
// for local development against several platforms at once.

const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

function ensureDir(dir) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
}

function run(cmd, args, opts) {
  console.log('> ' + [cmd].concat(args).join(' '));
  const r = spawnSync(cmd, args, Object.assign({ stdio: 'inherit' }, opts));
  if (r.error) throw r.error;
  if (r.status !== 0) {
    throw new Error(`${cmd} exited with ${r.status}`);
  }
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const [key, inline] = argv[i].split('=');
    if (!key.startsWith('--')) continue;
    const name = key.slice(2);
    out[name] = inline !== undefined ? inline : argv[++i];
  }
  return out;
}

function hostTarget() {
  return {
    goos: process.platform === 'win32' ? 'windows' : process.platform,
    goarch: process.arch === 'x64' ? 'amd64' : process.arch,
  };
}

function version() {
  try {
    const pkg = JSON.parse(fs.readFileSync(path.resolve(__dirname, '..', 'package.json'), 'utf8'));
    return pkg.version || 'dev';
  } catch {
    return 'dev';
  }
}

function build(target, outPath) {
  const cmdPath = path.join('..', 'cmd', 'hclschema-cli');
  const env = Object.assign({}, process.env, {
    GOOS: target.goos,
    GOARCH: target.goarch,
    CGO_ENABLED: '0',
  });
  run('go', [
    'build',
    '-trimpath',
    '-ldflags', `-s -w -X main.Version=${version()}`,
    '-o', outPath,
    cmdPath,
  ], { env });
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const root = path.resolve(__dirname, '..');
  const binDir = path.join(root, 'bin');
  ensureDir(binDir);

  if (args.goos || args.goarch) {
    const target = { goos: args.goos, goarch: args.goarch };
    if (!target.goos || !target.goarch) {
      throw new Error('--goos and --goarch must be given together');
    }
    const name = target.goos === 'windows' ? 'hclschema-cli.exe' : 'hclschema-cli';
    build(target, path.join(binDir, name));
    console.log(`built ${target.goos}/${target.goarch}`);
    return;
  }

  const targets = [
    { goos: 'windows', goarch: 'amd64' },
    { goos: 'windows', goarch: 'arm64' },
    { goos: 'linux', goarch: 'amd64' },
    { goos: 'linux', goarch: 'arm64' },
    { goos: 'darwin', goarch: 'amd64' },
    { goos: 'darwin', goarch: 'arm64' },
  ];
  for (const t of targets) {
    const outDir = path.join(binDir, `${t.goos}-${t.goarch}`);
    ensureDir(outDir);
    const name = t.goos === 'windows' ? 'hclschema-cli.exe' : 'hclschema-cli';
    try {
      build(t, path.join(outDir, name));
    } catch (e) {
      console.warn(`warning: ${t.goos}/${t.goarch} failed: ${e.message || e}`);
    }
  }

  // A flat copy for the host, so `yarn watch` style development finds a binary
  // without going through the packaging script.
  const host = hostTarget();
  const hostName = host.goos === 'windows' ? 'hclschema-cli.exe' : 'hclschema-cli';
  try {
    build(host, path.join(binDir, hostName));
  } catch (e) {
    console.warn('warning: host build failed: ' + (e.message || e));
  }

  console.log('go-cli build complete (multi-target)');
}

main();

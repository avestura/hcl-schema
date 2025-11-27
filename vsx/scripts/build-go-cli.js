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
  if (r.status !== 0) process.exit(r.status);
}

(async function main() {
  const root = path.resolve(__dirname, '..'); // vsx
  const binDir = path.join(root, 'bin');
  ensureDir(binDir);
  const goCmd = 'go';
  const cmdPath = path.join('..', 'cmd', 'hclschema-cli');

  // Targets to build for. We always build the host/native binary as well.
  const targets = [
    { goos: process.platform === 'win32' ? 'windows' : process.platform, goarch: process.arch === 'x64' ? 'amd64' : process.arch },
    { goos: 'windows', goarch: 'amd64' },
    { goos: 'linux', goarch: 'amd64' },
    { goos: 'darwin', goarch: 'amd64' },
    { goos: 'linux', goarch: 'arm64' },
    { goos: 'darwin', goarch: 'arm64' }
  ];

  for (const t of targets) {
    const outDir = path.join(binDir, t.goos + '-' + t.goarch);
    ensureDir(outDir);
    const isWin = t.goos === 'windows';
    const outName = isWin ? 'hclschema-cli.exe' : 'hclschema-cli';
    const outPath = path.join(outDir, outName);
    const env = Object.assign({}, process.env, { GOOS: t.goos, GOARCH: t.goarch });
    try {
      run(goCmd, ['build', '-o', outPath, cmdPath], { env });
    } catch (e) {
      console.warn(`warning: failed to build for ${t.goos}/${t.goarch}: ${e.message || e}`);
    }
  }

  // Also produce a top-level non-qualified binary for the host platform for backwards compatibility
  try {
    const hostOut = path.join(binDir, process.platform === 'win32' ? 'hclschema-cli.exe' : 'hclschema-cli');
    const hostEnv = Object.assign({}, process.env, { GOOS: process.platform === 'win32' ? 'windows' : process.platform, GOARCH: process.arch === 'x64' ? 'amd64' : process.arch });
    run(goCmd, ['build', '-o', hostOut, cmdPath], { env: hostEnv });
  } catch (e) {
    console.warn('warning: failed to build host binary: ' + (e.message || e));
  }

  console.log('go-cli build complete (multi-target)');
})();

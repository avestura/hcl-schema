// Checks that a release tag agrees with the version in package.json.
//
// The marketplace takes the version from package.json, not from the tag, so
// without this a tag could say v0.2.0 while 0.1.0 is what actually gets
// published — and the mismatch would only surface after the release is live
// and no longer republishable under that version.
//
// Usage: node scripts/check-version.js <tag>
//
//   v1.2.3      -> stable release of 1.2.3
//   v1.2.3-pre  -> pre-release of 1.2.3
//
// Prints `version=` and `prerelease=` lines for a workflow to read.

const fs = require('fs');
const path = require('path');

const tag = process.argv[2];
if (!tag) {
  console.error('usage: node scripts/check-version.js <tag>');
  process.exit(2);
}

// The marketplace only accepts x.y.z, so a tag cannot carry a semver
// pre-release suffix like -rc.1; `-pre` is a marker for this script, not part
// of the published version.
const match = /^v(\d+\.\d+\.\d+)(-pre)?$/.exec(tag);
if (!match) {
  console.error(
    `Tag ${tag} is not a release tag.\n` +
      'Expected v<major>.<minor>.<patch>, optionally suffixed with -pre, for example v1.2.3 or v1.2.3-pre.',
  );
  process.exit(1);
}

const [, version, preMarker] = match;
const prerelease = preMarker !== undefined;

const pkgPath = path.resolve(__dirname, '..', 'package.json');
const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));

if (pkg.version !== version) {
  console.error(
    `Tag ${tag} does not match vsx/package.json.\n` +
      `  tag:          ${version}\n` +
      `  package.json: ${pkg.version}\n\n` +
      'Bump the version in vsx/package.json, commit, then retag.',
  );
  process.exit(1);
}

// VS Code uses an odd minor version to mark pre-release builds, so that a
// later stable release can take the next even minor without going backwards.
if (prerelease && Number(version.split('.')[1]) % 2 === 0) {
  console.warn(
    `Warning: ${version} has an even minor version but is being published as a pre-release.\n` +
      'VS Code convention reserves odd minor versions for pre-releases so a subsequent\n' +
      'stable release can use the next even one. See https://code.visualstudio.com/api/working-with-extensions/publishing-extension#prerelease-extensions',
  );
}

console.log(`Tag ${tag} matches vsx/package.json ${version}.`);

const output = process.env.GITHUB_OUTPUT;
if (output) {
  fs.appendFileSync(output, `version=${version}\nprerelease=${prerelease}\n`);
}

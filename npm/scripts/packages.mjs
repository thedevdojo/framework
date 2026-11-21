#!/usr/bin/env node
// Makes the npm packages for one release, ready to publish. It publishes
// nothing itself: the release workflow runs `npm publish` on what it writes.
//
//   node npm/scripts/packages.mjs --version 1.2.0 --dist dist --out npm/out
//   node npm/scripts/packages.mjs --version 1.2.0 --binary darwin-arm64=bin/devdojo --out npm/out
//
// --dist is goreleaser's dist/ folder: the binaries are found through its
// artifacts.json. --binary names one binary by hand instead (repeatable), to
// try a package locally with a binary built on this machine.
//
// It writes one folder per package into --out:
//   devdojo-darwin-arm64/ … devdojo-win32-x64/   one binary each, limited
//                                                to its os and cpu
//   devdojo/                                     the `devdojo` command, every
//                                                platform package an
//                                                optionalDependency at this
//                                                same version
// Publish the platform packages first, then devdojo/.

import { chmodSync, copyFileSync, cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const license = join(here, '..', '..', 'LICENSE');

// Every platform a package is made for: goreleaser's os and arch, and npm's.
export const platforms = [
  { goos: 'darwin', goarch: 'arm64', os: 'darwin', cpu: 'arm64' },
  { goos: 'darwin', goarch: 'amd64', os: 'darwin', cpu: 'x64' },
  { goos: 'linux', goarch: 'arm64', os: 'linux', cpu: 'arm64' },
  { goos: 'linux', goarch: 'amd64', os: 'linux', cpu: 'x64' },
  { goos: 'windows', goarch: 'amd64', os: 'win32', cpu: 'x64' },
];

const usage = 'usage: node npm/scripts/packages.mjs --version X.Y.Z (--dist dist | --binary <os>-<cpu>=<path> …) --out <folder>';

function die(message) {
  process.stderr.write(message + '\n' + usage + '\n');
  process.exit(1);
}

function parseArgs(argv) {
  const args = { binaries: {} };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) die(arg + ' needs a value');
      return argv[++i];
    };
    if (arg === '--version') args.version = value().replace(/^v/, '');
    else if (arg === '--dist') args.dist = value();
    else if (arg === '--out') args.out = value();
    else if (arg === '--binary') {
      const [key, path] = value().split('=');
      if (!key || !path) die('--binary takes <os>-<cpu>=<path>, like darwin-arm64=bin/devdojo');
      args.binaries[key] = path;
    } else die('unknown option ' + arg);
  }
  if (!args.version || !/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(args.version)) die('--version needs a version like 1.2.0');
  if (!args.out) die('--out is needed');
  if (!args.dist && Object.keys(args.binaries).length === 0) die('--dist or --binary is needed');
  return args;
}

// The binary goreleaser built for each platform, from its artifacts.json.
function fromDist(dist) {
  const file = join(dist, 'artifacts.json');
  if (!existsSync(file)) die(file + ' is not there: is ' + dist + " goreleaser's dist folder?");
  const found = {};
  for (const a of JSON.parse(readFileSync(file, 'utf8'))) {
    if (a.type !== 'Binary') continue;
    const p = platforms.find((p) => p.goos === a.goos && p.goarch === a.goarch);
    if (p) found[p.os + '-' + p.cpu] = a.path;
  }
  return found;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const binaries = args.dist ? fromDist(args.dist) : args.binaries;
  for (const key of Object.keys(args.binaries)) {
    if (!platforms.some((p) => p.os + '-' + p.cpu === key)) die('no platform called ' + key);
  }
  if (args.dist) {
    const missing = platforms.filter((p) => !binaries[p.os + '-' + p.cpu]);
    if (missing.length) die('no binary for ' + missing.map((p) => p.os + '-' + p.cpu).join(', ') + ' in ' + args.dist);
  }

  const out = resolve(args.out);
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });

  const main = JSON.parse(readFileSync(join(here, '..', 'devdojo', 'package.json'), 'utf8'));
  main.version = args.version;
  main.optionalDependencies = {};

  for (const p of platforms) {
    const key = p.os + '-' + p.cpu;
    const name = 'devdojo-' + key;
    main.optionalDependencies[name] = args.version;
    const binary = binaries[key];
    if (!binary) continue; // a local try with only some of them
    const dir = join(out, 'devdojo-' + key);
    const file = p.os === 'win32' ? 'devdojo.exe' : 'devdojo';
    mkdirSync(join(dir, 'bin'), { recursive: true });
    copyFileSync(binary, join(dir, 'bin', file));
    chmodSync(join(dir, 'bin', file), 0o755);
    const pkg = {
      name,
      version: args.version,
      description: 'The devdojo binary for ' + p.os + ' ' + p.cpu + '. Install devdojo, not this.',
      license: main.license,
      homepage: main.homepage,
      repository: main.repository,
      os: [p.os],
      cpu: [p.cpu],
      files: ['bin/' + file],
      preferUnplugged: true,
    };
    writeFileSync(join(dir, 'package.json'), JSON.stringify(pkg, null, 2) + '\n');
    copyFileSync(license, join(dir, 'LICENSE'));
  }

  const dir = join(out, 'devdojo');
  cpSync(join(here, '..', 'devdojo'), dir, { recursive: true });
  writeFileSync(join(dir, 'package.json'), JSON.stringify(main, null, 2) + '\n');
  copyFileSync(license, join(dir, 'LICENSE'));
  process.stdout.write('Wrote the packages of ' + args.version + ' into ' + out + '\n');
}

main();

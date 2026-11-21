#!/usr/bin/env node
// The `devdojo` command, as npm installs it. There is no engine here: npm
// also installed the one package of devdojo-<os>-<cpu> that fits
// this machine (they are optionalDependencies, each limited to its os and
// cpu), and this runs the binary inside it with the same arguments, the same
// terminal and the same exit code.
//
// One command is answered here instead: `devdojo verify <key>`, which
// devdojo.com's dashboard asks new authors to run (lib/verify.js).
'use strict';

const { spawnSync } = require('child_process');

const packages = {
  'darwin arm64': 'devdojo-darwin-arm64',
  'darwin x64': 'devdojo-darwin-x64',
  'linux arm64': 'devdojo-linux-arm64',
  'linux x64': 'devdojo-linux-x64',
  'win32 x64': 'devdojo-win32-x64',
};

function fail(message) {
  process.stderr.write('devdojo: ' + message + '\n');
  process.exit(1);
}

function runBinary() {
  const name = packages[process.platform + ' ' + process.arch];
  if (!name) {
    fail(
      'there is no devdojo for ' + process.platform + ' ' + process.arch + ' on npm yet. ' +
        'Download it from https://github.com/thedevdojo/framework/releases instead.'
    );
  }

  const file = process.platform === 'win32' ? 'devdojo.exe' : 'devdojo';
  let binary;
  try {
    binary = require.resolve(name + '/bin/' + file);
  } catch (e) {
    fail(
      name + ' is not installed, and it holds the program itself. It is an optional ' +
        'dependency of devdojo, so an install with optional dependencies turned ' +
        'off (--omit=optional, --no-optional) leaves it out. Install again without that: ' +
        'npm install -g devdojo'
    );
  }

  const result = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' });
  if (result.error) {
    fail('could not run ' + binary + ': ' + result.error.message);
  }
  if (result.signal) {
    // Stopped by a signal (Ctrl+C on `devdojo serve`): stop the same way.
    process.kill(process.pid, result.signal);
  }
  process.exit(result.status === null ? 1 : result.status);
}

if (process.argv[2] === 'verify') {
  require('../lib/verify.js')
    .run(process.argv.slice(3))
    .then((code) => process.exit(code));
} else {
  runBinary();
}

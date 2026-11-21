// `devdojo verify <key>`: confirms a devdojo.com account is a real person.
//
// The devdojo.com dashboard asks a new author to run `npx devdojo verify
// <key>`. That command came from the devdojo npm package before the package
// became DevDojo Framework's installer, so the package keeps answering it
// itself, without the engine: it sends the key to POST {site}/verify and says
// what the site answered.
//
//   --env=local|dev|staging|production   which devdojo.com to ask (default production)
//   DEVDOJO_API_URL                      ask this address instead (for tests)
'use strict';

const http = require('http');
const https = require('https');

const sites = {
  local: 'https://devdojo.test',
  dev: 'https://devdojo.test',
  staging: 'https://staging.devdojo.com',
  production: 'https://devdojo.com',
  prod: 'https://devdojo.com',
};

function usage() {
  return [
    'Usage: npx devdojo verify <key>',
    '',
    'Confirms your devdojo.com account with the key from your dashboard.',
    '  --env=local    ask your local devdojo.test instead of devdojo.com',
  ].join('\n');
}

// POSTs {key} as JSON and resolves with the status and the parsed body.
function post(address, key, insecure) {
  const url = new URL('/verify', address);
  const body = JSON.stringify({ key });
  const client = url.protocol === 'http:' ? http : https;
  return new Promise((resolve, reject) => {
    const req = client.request(
      url,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', 'Content-Length': Buffer.byteLength(body) },
        // Local and staging sites use certificates a machine may not trust.
        rejectUnauthorized: !insecure,
        timeout: 20000,
      },
      (res) => {
        let data = '';
        res.setEncoding('utf8');
        res.on('data', (c) => (data += c));
        res.on('end', () => {
          let parsed = null;
          try {
            parsed = JSON.parse(data);
          } catch (e) {
            // Not JSON: an error page. The status says enough.
          }
          resolve({ status: res.statusCode, body: parsed });
        });
      }
    );
    req.on('timeout', () => req.destroy(new Error('no answer after 20 seconds')));
    req.on('error', reject);
    req.end(body);
  });
}

// Runs the command with the arguments after `verify`; resolves with the exit code.
async function run(args) {
  let env = 'production';
  const rest = [];
  for (const arg of args) {
    if (arg.startsWith('--env=')) env = arg.slice('--env='.length).toLowerCase();
    else rest.push(arg);
  }

  const key = rest[0];
  if (!key || key === '--help' || key === '-h') {
    process.stderr.write(usage() + '\n');
    return 1;
  }

  const address = process.env.DEVDOJO_API_URL || sites[env] || sites.production;
  const insecure = !process.env.DEVDOJO_API_URL && env !== 'production' && env !== 'prod';

  let answer;
  try {
    answer = await post(address, key, insecure);
  } catch (error) {
    process.stderr.write('✗ Could not reach ' + address + ' (' + (error.code || error.message) + '). Check your connection and try again.\n');
    return 1;
  }

  const { status, body } = answer;
  if (status >= 200 && status < 300 && body && body.success) {
    process.stdout.write('✓ Your DevDojo account has been verified. You can carry on in your browser.\n');
    return 0;
  }
  if (status === 429) {
    process.stderr.write('✗ Too many failed attempts. Try again in 30 minutes.\n');
  } else if (body && body.message) {
    process.stderr.write('✗ ' + body.message + '\n');
  } else {
    process.stderr.write('✗ Verification failed (' + address + ' answered ' + status + ').\n');
  }
  return 1;
}

module.exports = { run };

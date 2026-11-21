// `devdojo verify <key>`: the one command the npm package answers itself.
// devdojo.com asks a new author to run `npx devdojo verify <key>`, which the
// devdojo package did before it became DevDojo Framework's installer, so it
// still has to work. Run with: node --test npm/test/
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const launcher = fileURLToPath(new URL('../devdojo/bin/devdojo.js', import.meta.url));

// A stand-in for devdojo.com's POST /verify, answering with `status` and `body`.
async function server(status, body) {
  const seen = [];
  const s = createServer((req, res) => {
    let data = '';
    req.on('data', (c) => (data += c));
    req.on('end', () => {
      seen.push({ method: req.method, url: req.url, type: req.headers['content-type'], body: data });
      res.writeHead(status, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(body));
    });
  });
  await new Promise((r) => s.listen(0, '127.0.0.1', r));
  return { url: 'http://127.0.0.1:' + s.address().port, seen, close: () => s.close() };
}

function run(args, env = {}) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [launcher, ...args], { env: { ...process.env, ...env } });
    let out = '';
    child.stdout.on('data', (c) => (out += c));
    child.stderr.on('data', (c) => (out += c));
    child.on('close', (code) => resolve({ code, out }));
  });
}

test('a good key is sent to /verify and reported as verified', async () => {
  const api = await server(200, { success: true });
  const { code, out } = await run(['verify', 'abc123'], { DEVDOJO_API_URL: api.url });
  api.close();
  assert.equal(code, 0, out);
  assert.match(out, /Your DevDojo account has been verified/);
  assert.equal(api.seen.length, 1);
  assert.equal(api.seen[0].method, 'POST');
  assert.equal(api.seen[0].url, '/verify');
  assert.equal(api.seen[0].type, 'application/json');
  assert.deepEqual(JSON.parse(api.seen[0].body), { key: 'abc123' });
});

test("the server's message is shown when a key is refused", async () => {
  const api = await server(422, { success: false, message: 'That key has expired.' });
  const { code, out } = await run(['verify', 'old'], { DEVDOJO_API_URL: api.url });
  api.close();
  assert.equal(code, 1);
  assert.match(out, /That key has expired\./);
});

test('too many tries says to wait', async () => {
  const api = await server(429, { success: false });
  const { code, out } = await run(['verify', 'abc'], { DEVDOJO_API_URL: api.url });
  api.close();
  assert.equal(code, 1);
  assert.match(out, /Too many failed attempts/);
});

test('--env=local is taken out of the arguments, not sent as the key', async () => {
  const api = await server(200, { success: true });
  const { code } = await run(['verify', '--env=local', 'abc'], { DEVDOJO_API_URL: api.url });
  api.close();
  assert.equal(code, 0);
  assert.deepEqual(JSON.parse(api.seen[0].body), { key: 'abc' });
});

test('no key prints how to use it and sends nothing', async () => {
  const api = await server(200, { success: true });
  const { code, out } = await run(['verify'], { DEVDOJO_API_URL: api.url });
  api.close();
  assert.equal(code, 1);
  assert.match(out, /npx devdojo verify <key>/);
  assert.equal(api.seen.length, 0);
});

test('a server that is not there is reported, not thrown', async () => {
  const { code, out } = await run(['verify', 'abc'], { DEVDOJO_API_URL: 'http://127.0.0.1:1' });
  assert.equal(code, 1);
  assert.match(out, /Could not reach http:\/\/127\.0\.0\.1:1/);
});

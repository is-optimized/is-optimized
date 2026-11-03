import test from 'node:test';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, rm, symlink } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

async function installedBin(t) {
  const root = await mkdtemp(join(tmpdir(), 'is-optimized-bin-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const binDir = join(root, 'node_modules', '.bin');
  await mkdir(binDir, { recursive: true });
  const bin = join(binDir, 'is-optimized');
  await symlink(fileURLToPath(new URL('./cli.mjs', import.meta.url)), bin);
  return args => new Promise((resolve, reject) => {
    execFile(bin, args, {
      timeout: 10000,
      maxBuffer: 256 * 1024,
      env: { ...process.env, PATH: dirname(process.execPath) + delimiter + (process.env.PATH || '') },
    }, (error, stdout, stderr) => {
      if (error && typeof error.code !== 'number') reject(error);
      else resolve({ code: error?.code ?? 0, stdout, stderr });
    });
  });
}

async function auditAPI(t, handler) {
  const requests = [];
  const server = createServer(async (request, response) => {
    try {
      let body = '';
      for await (const chunk of request) body += chunk;
      requests.push({ method: request.method, url: request.url, body: body ? JSON.parse(body) : null });
      const result = handler(request, requests);
      response.writeHead(result.status ?? 200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify(result.body));
    } catch (error) {
      response.writeHead(500, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ error: error.message }));
    }
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  t.after(() => new Promise(resolve => {
    server.close(resolve);
    server.closeAllConnections?.();
  }));
  return { origin: `http://127.0.0.1:${server.address().port}`, requests };
}

const reportWithScore = score => ({
  domain: 'example.com',
  pillars: [{ id: 'agent_readiness', name: 'Agent readiness', score, coverage: score === null ? 0.5 : 1 }],
  findings: [{ id: 'fixture', status: 'pass', evidence: 'Local HTTP fixture only.' }],
});

test('installed .bin symlink prints help and runs the main entry point', async t => {
  const runBin = await installedBin(t);
  const result = await runBin(['--help']);
  assert.equal(result.code, 0);
  assert.match(result.stdout, /^Usage: is-optimized <domain>/);
  assert.match(result.stdout, /--fail-under/);
  assert.equal(result.stderr, '');
});

test('installed .bin symlink submits, polls, and emits report JSON', async t => {
  const runBin = await installedBin(t);
  const report = reportWithScore(84);
  const api = await auditAPI(t, request => {
    if (request.method === 'POST' && request.url === '/api/v1/scans') {
      return { status: 202, body: { id: 'fixture-123', state: 'queued' } };
    }
    if (request.method === 'GET' && request.url === '/api/v1/scans/fixture-123') {
      return { body: { state: 'complete', report } };
    }
    return { status: 404, body: { error: 'Unexpected request' } };
  });
  const result = await runBin(['example.com', '--json', '--api', api.origin, '--timeout', '5']);
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), report);
  assert.equal(result.stderr, '');
  assert.deepEqual(api.requests, [
    { method: 'POST', url: '/api/v1/scans', body: { domain: 'example.com' } },
    { method: 'GET', url: '/api/v1/scans/fixture-123', body: null },
  ]);
});

test('installed .bin symlink preserves threshold and unavailable-score exits', async t => {
  const runBin = await installedBin(t);
  let report = reportWithScore(79);
  const api = await auditAPI(t, () => ({ body: report }));
  for (const scenario of [
    { name: 'below', score: 79, threshold: '80', code: 1 },
    { name: 'meets', score: 79, threshold: '79', code: 0 },
    { name: 'unavailable', score: null, threshold: '79', code: 2 },
  ]) {
    await t.test(scenario.name, async () => {
      report = reportWithScore(scenario.score);
      const result = await runBin(['example.com', '--json', '--fail-under', scenario.threshold, '--api', api.origin, '--timeout', '5']);
      assert.equal(result.code, scenario.code, result.stderr);
      assert.deepEqual(JSON.parse(result.stdout), report);
      if (scenario.code === 2) assert.match(result.stderr, /insufficient evidence/);
      else assert.equal(result.stderr, '');
    });
  }
  assert.equal(api.requests.length, 3);
  assert.ok(api.requests.every(request => request.method === 'POST' && request.url === '/api/v1/scans'));
});

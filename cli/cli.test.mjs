import test from 'node:test';
import assert from 'node:assert/strict';
import { options, run } from './cli.mjs';

const report = score => ({ domain: 'example.com', findings: [], pillars: [{ id: 'agent_readiness', name: 'Agent readiness', score, coverage: 1 }] });
const response = data => ({ ok: true, json: async () => data });

test('cached JSON output and thresholds use the existing Agent readiness pillar', async () => {
  for (const [score, expected] of [[87, 0], [40, 1], [null, 2]]) {
    let stdout = '', stderr = '';
    const code = await run(['example.com', '--json', '--fail-under', '80'], { out: s => stdout += s, err: s => stderr += s, fetch: async () => response(report(score)) });
    assert.equal(code, expected);
    assert.deepEqual(JSON.parse(stdout), report(score));
    if (score === null) assert.match(stderr, /insufficient evidence/);
  }
});

test('queued jobs are polled without duplicate scan submission', async () => {
  const calls = [];
  const data = [{ id: 'abc123', state: 'queued' }, { id: 'abc123', state: 'running' }, { id: 'abc123', state: 'completed', report: report(90) }];
  const code = await run(['example.com', '--json'], { out: () => {}, err: () => {}, pause: async () => {}, fetch: async (url, opt) => { calls.push([url, opt.method]); return response(data.shift()); } });
  assert.equal(code, 0);
  assert.equal(calls.filter(([, method]) => method === 'POST').length, 1);
  assert.equal(calls.length, 3);
});

test('invalid arguments and server errors return actionable failure without extra requests', async () => {
  assert.throws(() => options(['example.com', '--fail-under', '101']));
  assert.throws(() => options(['example.com', '--api', 'https://user:password@example.com']));
  assert.throws(() => options(['example.com', '--timeout', '0']));
  let message = '';
  const code = await run(['example.com'], { out: () => {}, err: s => message += s, fetch: async () => ({ ok: false, status: 429, headers: new Headers({ 'Retry-After': '30' }) }) });
  assert.equal(code, 2);
  assert.match(message, /retry after 30 seconds/);
});

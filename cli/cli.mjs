#!/usr/bin/env node
import { pathToFileURL } from 'node:url';

export function options(argv) {
  const out = { json: false, api: 'https://is-optimized.com', timeout: 370000 };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--json') out.json = true;
    else if (arg === '--help' || arg === '-h') out.help = true;
    else if (arg === '--fail-under' || arg === '--timeout' || arg === '--api') {
      const value = argv[++i];
      if (!value || value.startsWith('--')) throw Error(`${arg} requires a value`);
      if (arg === '--api') out.api = value;
      else if (arg === '--timeout') {
        const seconds = Number(value);
        if (!Number.isFinite(seconds) || seconds <= 0 || seconds > 600) throw Error('--timeout must be 1–600 seconds');
        out.timeout = seconds * 1000;
      } else {
        out.threshold = Number(value);
        if (!Number.isFinite(out.threshold) || out.threshold < 0 || out.threshold > 100) throw Error('--fail-under must be 0–100');
      }
    } else if (arg.startsWith('-') || out.domain) throw Error(`Unexpected argument: ${arg}`);
    else out.domain = arg;
  }
  if (!out.help && !out.domain) throw Error('A public domain is required');
  const api = new URL(out.api);
  if (!['http:', 'https:'].includes(api.protocol) || api.username || api.password || api.search || api.hash || api.pathname !== '/') throw Error('--api must be an HTTP(S) origin without credentials');
  out.api = api.origin;
  return out;
}

export async function run(argv, io = {}) {
  const out = io.out ?? (text => process.stdout.write(text));
  const err = io.err ?? (text => process.stderr.write(text));
  const fetcher = io.fetch ?? globalThis.fetch;
  const pause = io.pause ?? (ms => new Promise(resolve => setTimeout(resolve, ms)));
  try {
    const opt = options(argv);
    if (opt.help) {
      out('Usage: is-optimized <domain> [--json] [--fail-under 0–100] [--api https://is-optimized.com] [--timeout seconds]\nThe threshold applies to Agent readiness. Exit 0: meets threshold; 1: below; 2: error or insufficient evidence. Reports are public.\n');
      return 0;
    }
    const deadline = Date.now() + opt.timeout;
    async function request(path, body) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) throw Error('Scan wait timed out; retrieve the existing scan instead of resubmitting');
      const response = await fetcher(opt.api + path, {
        method: body ? 'POST' : 'GET',
        headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body: JSON.stringify(body) } : {}),
        signal: AbortSignal.timeout(Math.max(1, Math.min(15000, remaining))),
        redirect: 'error',
      });
      if (!response.ok) {
        const retry = response.headers?.get('Retry-After');
        throw Error(`Audit API returned HTTP ${response.status}${retry ? `; retry after ${retry} seconds` : ''}`);
      }
      return response.json();
    }
    let data = await request('/api/v1/scans', { domain: opt.domain });
    // A cache hit returns a report directly; a queued request returns an id.
    while (!data.report && !Array.isArray(data.findings)) {
      if (!data.id || !/^[a-zA-Z0-9_-]+$/.test(data.id)) throw Error('Audit API returned an invalid scan identifier');
      if (['failed', 'expired'].includes(data.state)) throw Error(data.error || `Scan ${data.state}`);
      if (data.state === 'partial') throw Error('Partial scan did not include a report');
      if (!opt.json) err(`Scan ${data.id}: ${data.state || 'queued'}${data.position ? ` (queue position ${data.position})` : ''}\n`);
      await pause(Math.min(1000, Math.max(0, deadline - Date.now())));
      data = await request(`/api/v1/scans/${encodeURIComponent(data.id)}`);
    }
    const report = data.report ?? data;
    if (!Array.isArray(report.pillars) || !Array.isArray(report.findings)) throw Error('Audit API response is not a report');
    if (opt.json) out(JSON.stringify(report) + '\n');
    else {
      out(`${report.domain}\n`);
      for (const pillar of report.pillars) out(`${pillar.name || pillar.id}: ${pillar.score == null ? 'insufficient evidence' : `${pillar.score}/100`} · coverage ${Math.round((pillar.coverage ?? 0) * 100)}%\n`);
      out(`${opt.api}/${encodeURIComponent(report.domain)}\n`);
    }
    if (opt.threshold !== undefined) {
      const score = report.pillars.find(p => p.id === 'agent_readiness')?.score;
      if (typeof score !== 'number' || !Number.isFinite(score)) { err('Agent readiness has insufficient evidence; threshold cannot be evaluated.\n'); return 2; }
      return score < opt.threshold ? 1 : 0;
    }
    return 0;
  } catch (error) {
    err(`is-optimized: ${error.message}\n`);
    return 2;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) process.exitCode = await run(process.argv.slice(2));

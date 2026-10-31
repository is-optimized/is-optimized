# Verification and release record

## Current production baseline — 2026-10-30

Production was observed healthy on the previously accepted Nanos image, application revision `513c41ed5ff1a1f64032f8a79261e833346ea55c`. The deployment receipt records live health, report persistence, and HTML/PDF/latest-report routes. The initial deployment failure's exact cause remains unconfirmed; recovery is documented in [the cold-start receipt](cold-start-recovery-20261029.md).

SMS is disabled in production. The owner reported that the existing Twilio A2P campaign remained in review on 2026-10-31. No live message was sent.

## Merged release — PR #6, 2026-10-31

- PR branch: `codex/release-ready-sms-20261031`.
- Reviewed head: `afc2ed5b29c2ff1e5f8f39931bc97700f41910e6`, based on `d5f6b1feb88ea7b2c2deda76f1f2086b4668fe16`.
- Merge commit: `6c19d82cf67468bf0ab10578a696e2b31d603ccf`. The merge commit is an ancestor of `origin/main`; its tree matches the reviewed PR head.
- Candidate work includes the phone-only SMS request form with optional marketing consent, approved copy, sender-number matching, 32-page/two-hop/sitemap scan selection, a 60-second scanner budget, concurrent aggregate byte accounting, compact stored page references, fetch-time reporting in HTML/PDF, queue position and measured wait display, and operational metrics.
- Independent read-only review returned `REQUEST_CHANGES` twice, identifying deadline/persistence, report-size, aggregate-byte-budget, queue-progress, stale-scope, and FIFO-ordering issues. These were addressed. Final source review approved the revised code with no remaining source findings.
- `gofmt` and `git diff --check` passed. Local Go tests/builds were held because the host admission sample showed 0–14% CPU idle, below the required 20%.
- GitHub Actions run `41245081658` passed on the reviewed PR head: `go test -race ./...`, `go vet ./...`, and the static Linux build. These CI results do not qualify Nanos runtime or target-VM capacity.
- No candidate Nanos image has been built or boot-tested. The prior production image is not evidence for this candidate. No GCP, Cloudflare, Twilio, or production changes were made.

The 8 page fetches per scan, 16 outbound requests per instance, 4 scan workers, and 64-job storage guard are implementation limits, not measured e2-micro capacity. The 5-minute queue wait and 60-second scan budget are product targets; run a controlled e2-micro fixture benchmark before claiming capacity or changing these limits.

## Remaining qualification gates

The source review, required pull-request checks, guarded rebase merge, and landed-tree check for PR #6 are complete. The remaining gates below are separate from source merge and CI.

## Separate gates before SMS or deployment

- Build and boot-test the exact Nanos candidate, then qualify GCP storage, identity, TLS, HTML/PDF rendering, and resource behavior.
- Run the controlled same-region e2-micro load test before qualifying queue capacity or throughput.
- Wait for Twilio campaign approval; configure the pinned Secret Manager reference and approved sender, then perform an authorized test-number delivery and callback check.
- Keep production SMS disabled until those provider gates pass. No deployment is included in this release task.

## Methodology provenance

Reports identify their own methodology and selection-policy versions. Previous v1/v2 report records remain unchanged; the expanded selection candidate uses `2026-10-agentready-v3` and `home-links-sitemap-two-hop-v2`. Independent checks are not claims of parity with the hosted Is Agentic service.

# Verification map and outstanding acceptance

## Source candidate

The implementation is prepared on `feat/mvp`. The RFC is already merged via PR1. Do not treat this document, source review, or focused scanner tests as integrated build or deployment evidence.

## Required checks once capacity is explicitly re-admitted

1. Run the required host-capacity sampling and acquire the shared build lease. The earlier refusal is terminal for automatic retries; obtain a new authorized admission before resuming.
2. Run `go test -race ./...` and `go vet ./...` with the shared-host resource limits. Release the exact lease on completion/failure.
3. Build the static Linux binary and record size. Run the local application using an isolated temporary data directory, verify the browser flow, and inspect HTML and PDF reports.
4. Reconcile any failures, rerun affected checks, and obtain independent review of the final exact base/head. Only then open the implementation PR and use guarded rebase merge after its required gates.

## Fixture coverage prepared

- Scanner: DNS pinning/private address rejection, redirect robots ordering, page/depth/concurrency bounds, request/discovery/byte limits, unknown evidence and scoring coverage.
- Server: scan-to-HTML/PDF, escaped target text, cache reuse, trusted proxy attribution, stale worker nonpublication, PDF retry without rescan.
- SMS: signature tampering, token reuse with distinct message IDs, durable pending recovery, STOP suppression, interrupted sending without automatic resend, provider acceptance versus suppression/unknown.

Only focused scanner tests have run. The integrated server/messaging fixtures and full binary have not been executed. The UI and PDF have not been visually inspected. Gofmt parsing and diff whitespace checks are not type checking or runtime acceptance.

## Separate external gates

- Wazi plan conformance requires a qualified mapped checkout, reader CLI, and validator.
- Upstream hosted scanner source/behavior remains unavailable; independent methodology is not parity.
- Pulumi schema/preview, identity/resource reconciliation, cost review, and apply require appropriate environment and deployment authorization.
- Pinned OPS/Nanos image construction and boot; metadata, ADC, Secret Manager, Firestore, GCS, fonts, DNS and TLS need runtime qualification.
- Twilio real test-number delivery and callbacks require configured sender/account and explicit live-send authority. No messages were sent during this work.
- Owner-approved disclosure, retention/cleanup, and future shared suppression ownership remain production inputs.

## Current design candidate (2026-10-27, supersedes source status above)

Work is now on isolated `design/astra-finish`, based on `4b4ad444aab6ec47f2f134a93c41065e12cdb911`. Read the new devlog entry for the exact evidence tiers. The initial integrated race attempt failed on an unused import (fixed in source); scanner and messaging passed. The final revision has not been compiled or tested because the next build-phase capacity check refused execution. Do not treat the prepared render/SMS regression fixtures as executed tests.

New fixtures cover approved request/reply text, optional/invalid names, disabled-SMS report access, SVG serving boundaries, immutable evidence after both renderers, HTML escaping, presentation priority, and long-PDF pagination. `DESIGN_ARTIFACT_DIR=<task-artifacts>` on `go test ./internal/render` exports explicitly synthetic report HTML/PDF/JSON once execution is reauthorized and admitted. Do not run it as an automatic retry in this session.

Coordinator acceptance after fresh authorization/admission:

1. Use BuildOffload cache/tmp, `GOMAXPROCS=2`, `-p 1`, and the exact owned shared lease. Run final `go test -race ./...`, `go vet ./...`, and static Linux build. Record full source head and toolchain version.
2. Run the local Go app with isolated data, `SMS_ENABLED=false`, and loopback `LISTEN_ADDR`. Exercise scan → result → immutable HTML/PDF; invalid domain, overload, unknown evidence, and timeout recovery. Simulated SMS fixtures only; no provider send.
3. Capture 1440px desktop, 390px and 320px mobile, and print/PDF pages. Check keyboard focus, 200% zoom, reduced motion, status announcements, overflow, empty/unknown scores, disabled SMS, and optional marketing. Inspect PDF text extraction, font embedding, page breaks, and actual links. Current static preview files help source review but do not satisfy these checks.
4. Reconcile independent review with the final diff; resolve findings before merge. Preserve guarded merge and any required Wazi/project gates. The old blocked Wazi draft is not newly validated.
5. Follow `deployment-candidate.md` for explicit project/budget, authentication/backend/resource identity, actual preview, image construction and Nanos boot qualification. Do not deploy a Linux-build-only artifact.

## Minimal design continuation — 2026-10-27

Owner feedback supersedes the editorial design and this lane's previous deployment assignment. Starting head: `48f54afe433f32d7669ba6ff8aebf8168cc6259c`, branch `design/astra-finish`. Astra owns design/frontend only; coordinator owns deployment. No cloud/auth/infra inspection or mutation, packaging, new implementation worker, capacity retry, heavy test/build, or browser binary is authorized in this continuation.

Current acceptance adds the staged landing → indeterminate progress → optional SMS delivery → domain report path, a bounded real public directory, flat evidence rows/accordions, and minimal dark branding. See `docs/brand.md` and `docs/public-directory.md`. Static preview states are under `/Volumes/BuildOffload/artifacts/is-optimized-astra-minimal-20261027/preview/` and have synthetic-only preview labeling. They are not Go-rendered output or screenshots. Final Go tests, store integration, browser behavior, and PDF appearance remain held regardless of source review.

## Compact consent/report continuation — 2026-10-27

Starting head `736605259e86c555081169f2e5fe4b9b7622ff86` on `design/compact-consent-report` includes the coordinator's phone-field/binding fix. This lane changes only consent presentation/client US-number handling, report presentation/PDF source, rendering fixtures, and design evidence. Backend phone/signature/token/suppression tests are preserved; no scanner/methodology/weight, startup, infrastructure, auth, or packaging changes.

Current static preview folder is `/Volumes/BuildOffload/artifacts/is-optimized-astra-report-20261027/preview/`; it includes enabled/disabled compact consent, handoff, all-three-score report, missing/withheld-pillar report, and `example.com/index.html`. Samples are clearly labeled synthetic in preview-only chrome. The interpreter is not Go template execution or browser acceptance.

Prepared additional rendering fixtures protect actual zero versus nil versus absent pillars, all check-status counts, unclassified/excluded evidence preservation, deterministic foundational/status priority, immutable input, empty evidence, and unsafe affected-page link escaping. These are unrun. Existing phone-binding tests were not removed or changed.

Required coordinator acceptance after fresh authorization/admission: final Go tests/vet/build, browser mobile/desktop consent validation and bypass, exact disclosure text/accessibility, matching-sender simulated flow, chart/text correspondence, clipboard/fallback, report accordions/print, and actual PDF generation/text extraction/links/pagination. No heavy execution, browser binary, admission retry, or fallback is permitted in this lane. Source review alone does not clear these gates.

## Sampled SEO continuation — 2026-10-27

Candidate branch: `feat/seo-observations`, isolated worktree `/Volumes/BuildOffload/worktrees/is-optimized-seo-20261027`. Base `197bbed80c32f6ddda87efb43231967c351364b7` includes the independently source-reviewed compact consent/report design. Code head `7a3a0c3572bc3c0d80cf2da341ca5839e08ed3fe` adds methodology v2, duplicate sampled metadata, heading structure, viewport declaration, canonical conflict evidence, requested/final URL distinction, sampled-page outcomes, and informational word-count/HSTS findings. Existing report renderers consume the saved findings in both formats; no new requests or crawl depth are introduced.

Independent reviewer `/root/rfc_review` approved the exact base/head above with no actionable correctness or scoring-safety blockers. This was source-only review. `gofmt` and `git diff --check` passed. New fixture tests are prepared in `internal/scanner/observations_test.go` but have not run. No build-admission retry, tests, binary/image build, cloud operation, deployment, or provider send occurred in this continuation. The existing terminal memory-pressure refusal remains in force.

Next authorized admission should execute the scanner fixtures first, then the existing integration/rendering gates above. Inspect the v2 findings in actual generated HTML and PDF, including duplicate affected pages, unobserved pages, and a changed canonical target. Methodology v1 reports remain immutable and must not be relabeled as v2. Do not merge or deploy this source candidate until required execution gates pass.

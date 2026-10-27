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

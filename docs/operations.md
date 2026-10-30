# Deployment and recovery

The web pilot is deployed. Read the [verified recovery receipt](cold-start-recovery-20261029.md#verified-recovery--2026-10-30-utc) for the accepted source, artifact and runtime evidence. SMS remains disabled; web-pilot acceptance does not establish SMS/provider acceptance.

## Runtime qualification

Pin Go, OPS, and Nanos using `infra/nanos/versions.json`. The release receipt qualifies Go 1.26.0, OPS 0.1.47 and Nanos 0.1.56 for the representative web path. Build the Linux ELF with CGO disabled, generate the nonsecret OPS configuration using `scripts/nanos-config.py --mode local|gcp --ca-dir <public-CA-only-directory> --image <absolute-task-image-path>`, and boot-test changed images before promotion. Package a raw disk archive accepted by GCP and supply its local path as the Pulumi `imageArchive` input. Do not upload/create cloud images through OPS commands; Pulumi owns the bucket object, image, and single VM.

GCP images include the Nanos `gcp`/`tls` console logger with log ID `is-optimized-startup`. IaC grants the existing runtime identity write-only logging permission. Inspect fixed startup-stage messages and sanitized error categories; never add credentials, metadata values or provider response bodies to application logs. Metadata bootstrap has a bounded sixty-second retry window; invalid configuration fails immediately. Desired RUNNING state is reconciled on Pulumi updates, not by a new background scheduler. Authenticated virtual-display screenshots are available, but were insufficient to diagnose the observed cold-start failures.

The current packaging configuration deliberately disables SMS. Secure runtime secret retrieval, workload identity, CA roots, clock behavior, Firestore/GCS connectivity and PDF font rendering must be demonstrated inside Nanos before enabling production. Never bake `TWILIO_AUTH_TOKEN` or a service-account private key into an image. Runtime environment injection is acceptable for local testing only; qualify the implemented ADC-authenticated Secret Manager retrieval path for the production image. Set TWILIO_AUTH_TOKEN_SECRET to a numeric resource version and grant the runtime identity access only to that secret.

## Infrastructure

`infra/Pulumi.yaml` defines one e2-micro VM with a regional external IPv4, dedicated VPC/subnet, Cloudflare-only TCP 80/443 ingress, runtime identity, Firestore database, and private report/image/TLS-cache buckets. Origin TLS uses ACME HTTP-01. There is no NAT, load balancer, forwarding rule, or managed instance group. The GCP provider is pinned; the stack enables required APIs without disabling them on destroy. Existing default Firestore databases and named buckets must be reconciled with state rather than blindly created. Review all IAM, retention, and replacement actions in a full preview before apply.

The single zonal instance can be interrupted during replacement and has no automatic failover. The persistent IP remains stable across VM replacement. Cloudflare DNS must reach the origin before production ACME certificates can issue; preserve Cloudflare Full SSL and do not use Flexible mode. After validating the public certificate chain, consider a separate change to Full (strict).

Supply actual project, region/zone, domain, reviewed image archive, machine size, and budget. The application retrieves nonsecret configuration, including the generated bucket name, from the Pulumi-managed instance metadata attribute. This allows one generic image without embedding cloud resource names. Add secret-access IAM and an approved managed-secret bootstrap before enabling SMS.

Only the reviewed Cloudflare IPv4 ranges may reach origin TCP 80/443. Configure `TRUSTED_PROXY_CIDRS` with those same ranges. Verify client attribution through real proxied requests before accepting per-client limits. Do not configure 0.0.0.0/0 or trust a client-provided prefix.

## Durable state

Firestore `records` holds the bounded scan queue, immutable evidence, attempt snapshots, published reports, latest pointers, report-request/consent records, sender quotas, and local suppression observations. All JSON record updates are transactions. Attempt artifacts are generation-specific; only a successfully fenced publishing generation can create the public report pointer. Interrupted publication replays idempotently.

PDF failures retry saved evidence rather than rescanning. At most three work attempts are made. Inspect failed queue entries and evidence before a manual recovery; do not bypass admission or rewrite an already-published report. Expired public reports return HTTP410; a new scan is an explicit separate action.

Pending SMS intents are dispatched from durable records. A request token is consumed once, including when resent with a new Twilio message ID. State `sending` older than a minute becomes `outcome-unknown`; it is never automatically resent. Provider callbacks can reconcile an uncertain send. For missing callbacks, use Twilio's message records under authorized read access, match sender/recipient/time and report-request callback correlation, then perform a reviewed recovery change. Do not assume an API timeout means nothing was sent.

Provider STOP suppression is always respected. This service also records observed STOP locally and does not automatically clear it on START. A shared company-wide voice/text suppression authority remains deferred and must be established before marketing or calls are introduced.

## Retention and monitoring

GCS has a proposed30-day artifact lifecycle. Public reads enforce report expiry. Firestore report/evidence deletion and consent/delivery retention are not yet automated; production needs an owner-approved retention policy and cleanup job before collecting real phone data. Do not treat artifact expiry as deletion of contact records.

Monitor queue saturation, failed/unknown scans, expired leases, rendering failures, pending/sending/unknown delivery age, suppression errors, HTTP5xx, and resource pressure. `/healthz` is process liveness only; separately probe durable storage/readiness from the deployment qualification procedure. Logs must never contain raw phone numbers, provider tokens, full SMS bodies, or full target HTML.

Record separate measurements for kernel boot, application readiness, GCP provisioning, p50/p95 scans, PDF rendering, memory, binary/image size, and costs. No timing claim is established by source preparation.

## Current deployment handoff

See [deployment candidate and cost handoff](deployment-candidate.md) for the current project, cost, packaging, and DNS cutover details. Source preparation is not proof of Pulumi preview/apply, Nanos boot, TLS issuance, or public acceptance; record each separately.

# Deployment and recovery

This is source preparation, not a deployed service. Review the current RFC and evidence record before accepting a release.

## Runtime qualification

Pin Go, OPS, and Nanos using `infra/nanos/versions.json`. OPS and Nanos are not installed/qualified by this repository. Build the Linux ELF with CGO disabled, generate the nonsecret OPS configuration using `scripts/nanos-config.py`, and qualify the pinned OPS local image workflow against its documentation. Package a raw disk archive accepted by GCP and supply its local path as the Pulumi `imageArchive` input. Do not upload/create cloud images through OPS commands; Pulumi owns the bucket object, image, instance template, and instance group.

The current packaging configuration deliberately disables SMS. Secure runtime secret retrieval, workload identity, CA roots, clock behavior, Firestore/GCS connectivity and PDF font rendering must be demonstrated inside Nanos before enabling production. Never bake `TWILIO_AUTH_TOKEN` or a service-account private key into an image. Runtime environment injection is acceptable for local testing only; qualify the implemented ADC-authenticated Secret Manager retrieval path for the production image. Set TWILIO_AUTH_TOKEN_SECRET to a numeric resource version and grant the runtime identity access only to that secret.

## Infrastructure

`infra/Pulumi.yaml` defines a VPC, private subnet, NAT, runtime identity, Firestore database, private report/image buckets, immutable image, managed instance group, health check, and HTTPS load balancer/certificate. The GCP provider is pinned. APIs must be enabled under the owning project's authority before use. Existing default Firestore databases must be reconciled with state rather than blindly created. Review all IAM and retention settings in a preview before apply.

The default is one zonal instance: replacement causes a possible interruption. More instances in this zonal group do not create cross-zone resilience. Multi-zone acceptance remains a deliberate topology change, not an implication of `instanceCount=2`.

Supply actual project, region/zone, domain, reviewed image archive, machine size, and budget. The application retrieves nonsecret configuration, including the generated bucket name, from the Pulumi-managed instance metadata attribute. This allows one generic image without embedding cloud resource names. Add secret-access IAM and an approved managed-secret bootstrap before enabling SMS.

Only GCP health-check/proxy ranges may reach backend TCP8080. Configure `TRUSTED_PROXY_CIDRS` to include the observed trusted ingress peer ranges **and the exact forwarding-rule address as /32**, because GCP appends the frontend address to X-Forwarded-For. The application walks the chain from the right until the first untrusted address. Validate this using actual ingress requests before accepting per-client limits. Do not configure 0.0.0.0/0 or trust a client-provided prefix.

## Durable state

Firestore `records` holds the bounded scan queue, immutable evidence, attempt snapshots, published reports, latest pointers, report-request/consent records, sender quotas, and local suppression observations. All JSON record updates are transactions. Attempt artifacts are generation-specific; only a successfully fenced publishing generation can create the public report pointer. Interrupted publication replays idempotently.

PDF failures retry saved evidence rather than rescanning. At most three work attempts are made. Inspect failed queue entries and evidence before a manual recovery; do not bypass admission or rewrite an already-published report. Expired public reports return HTTP410; a new scan is an explicit separate action.

Pending SMS intents are dispatched from durable records. A request token is consumed once, including when resent with a new Twilio message ID. State `sending` older than a minute becomes `outcome-unknown`; it is never automatically resent. Provider callbacks can reconcile an uncertain send. For missing callbacks, use Twilio's message records under authorized read access, match sender/recipient/time and report-request callback correlation, then perform a reviewed recovery change. Do not assume an API timeout means nothing was sent.

Provider STOP suppression is always respected. This service also records observed STOP locally and does not automatically clear it on START. A shared company-wide voice/text suppression authority remains deferred and must be established before marketing or calls are introduced.

## Retention and monitoring

GCS has a proposed30-day artifact lifecycle. Public reads enforce report expiry. Firestore report/evidence deletion and consent/delivery retention are not yet automated; production needs an owner-approved retention policy and cleanup job before collecting real phone data. Do not treat artifact expiry as deletion of contact records.

Monitor queue saturation, failed/unknown scans, expired leases, rendering failures, pending/sending/unknown delivery age, suppression errors, HTTP5xx, and resource pressure. `/healthz` is process liveness only; separately probe durable storage/readiness from the deployment qualification procedure. Logs must never contain raw phone numbers, provider tokens, full SMS bodies, or full target HTML.

Record separate measurements for kernel boot, application readiness, GCP provisioning, p50/p95 scans, PDF rendering, memory, binary/image size, and costs. No timing claim is established by source preparation.

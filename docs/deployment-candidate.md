# Deployment candidate and cost handoff — 2026-10-27

**Not deployed or production-ready.** Project and monthly budget approval have not arrived. No preview, apply, import, refresh, secret export, DNS change, or live message was performed. Current architecture remains one Go process in one Nanos instance on GCP, with Firestore and private GCS persistence.

## Identity and tool observations

- Configured gcloud project: `sire-production`. This is configuration, not owner approval or confirmed resource custody.
- Read-only project/resource queries failed with `Reauthentication failed. cannot prompt during non-interactive execution.` No interactive login or account switching was attempted. The batched instance/database reads failed for the same reason; no resource inventory was obtained.
- Pulumi CLI 3.226.0 exists, but `pulumi whoami --verbose` reported that a login/access token is required in noninteractive mode. Backend, stack identity, and existing state are unqualified. No stack was created or selected.
- Go installed: 1.26.2 darwin/arm64. The repository toolchain manifest says 1.26.0; reconcile the toolchain pin in the next admitted verification, rather than claiming a pinned build now.
- OPS and QEMU were not found on PATH. No installation, packaging execution, local VM, or cloud boot was attempted. Existing `infra/nanos/versions.json` pins remain unqualified. Nanos version availability needs verification against OPS release distribution; GitHub source tag lookup alone did not qualify that binary release.
- OPS 0.1.47 source tag resolved to `fc3195f46c54ad94a0bcce69373d67a4441e0955`. Its `build` command invokes provider `BuildImage`; GCP packaging names the single archive member `disk.raw`. This source observation is not execution evidence. The provider factory initializes the GCP provider even for a build; do not assume the GCP build path is credential-independent.

## Smallest credible initial topology

Retain the existing one-instance private backend, HTTPS load balancer with managed certificate, and Cloud NAT. This provides TLS without adding a proxy process inside the Go/Nanos application. One zonal `e2-small` is the initial candidate, not a measured sizing result. Replacement can interrupt service. There is no second instance, cross-zone claim, Node service, external renderer, or optional Cloudflare edge in this candidate.

At one VM, replacing NAT with a public VM address saves only about USD1.02/month in NAT gateway hourly charges; both options still pay for one IPv4 address. It also changes exposure and recovery assumptions. Keep the current private topology until identity and cost review; do not invent a different host merely to remove the load-balancer line item. A smaller VM requires measured Nanos memory headroom first.

### Planning estimate (USD, 730 hours/month)

| Component | Basis | Monthly planning amount |
| --- | --- | --- |
| One e2-small | Provisional allowance of USD0.02/hour; verify actual region/SKU rate before approval | 14.60 |
| HTTPS forwarding rules | USD0.025/hour for first five rules | 18.25 |
| Public NAT gateway, one VM | USD0.0014/hour | 1.02 |
| One NAT external IPv4 | USD0.005/hour | 3.65 |
| Fixed compute/network subtotal | VM line is an allowance, not a live quote | **37.52** |
| Boot disk, image/report storage, Firestore operations/storage, NAT/LB processing, internet transfer, logs, tax | Usage and exact sizing not yet qualified | Additional |

A **USD50/month planning envelope** is a proposal for discussion, not an approved ceiling or a hard cap. Traffic can exceed it. NAT processing is USD0.045/GiB in addition to transfer charges. Do not assume free tiers or credits in a shared production project. The current unpreviewed template does not constrain every variable billable item; inspect actual disk size, image retention, operations, and ingress/egress estimates before apply.

Pricing references checked 2026-10-27: [Cloud NAT](https://cloud.google.com/nat/pricing), [load balancing](https://cloud.google.com/load-balancing/pricing), [VPC IPv4](https://cloud.google.com/vpc/pricing), [Compute Engine pricing](https://cloud.google.com/products/compute/pricing). VM pricing is explicitly an allowance because the region-specific table was not qualified. This is not financial approval.

## Packaging preparation

`scripts/nanos-config.py` now emits local or metadata-driven GCP config with SMS off, two Go scheduler threads, a 768MiB Go memory target, an explicit raw-image output, and an explicit public CA bundle mapping. The memory target is not a hard process limit and is not measured acceptance. It refuses a CA directory containing additional files or an obvious private key. Do not map a home, Keychain, cloud credential directory, or environment file into an image.

Prepare a task-owned directory containing only a reviewed public `ca-certificates.crt`, then emit config, for example:

```sh
python3 scripts/nanos-config.py --mode local \
  --ca-dir /Volumes/BuildOffload/artifacts/is-optimized/ca \
  --image /Volumes/BuildOffload/artifacts/is-optimized/local.raw \
  > /Volumes/BuildOffload/artifacts/is-optimized/local.json
```

For GCP use `--mode gcp`; project and bucket values come from repository-managed instance metadata. Config generation does not build an image or grant execution. Before an admitted packaging run, verify the installed OPS version, Nanos binary release/hash, and the exact pinned tool's flags. The pinned source supports `ops build <ELF> -c <config> -t gcp --nanos-version <version>` but initializes the provider; do not run it against unknown credentials/state. Never run `ops image create` or `ops instance create`: repository Pulumi owns cloud effects. The produced GCP archive must contain `disk.raw`, then be hashed and supplied to the reviewed `imageArchive` input.

Required local and GCP evidence: static Linux ELF; actual Nanos boot; `/healthz`; DNS and public HTTPS with CA roots; clock validity; scan → immutable HTML/PDF; font/text/link checks; local persistence/restart; metadata configuration; ADC; Firestore/GCS access with the exact runtime identity; trusted ingress attribution; and restart recovery. No current evidence satisfies these Nanos gates.

## Before any preview/apply

1. Receive explicit project identity and monthly cost ceiling from the coordinator. Restore authentication interactively under owner control.
2. Identify the existing Pulumi backend/stack and resource ownership without importing, refreshing, or decrypting unknown shared state. Reconcile the hardcoded `(default)` Firestore database and generic `records` collection before creation or access in a shared project. Resolve whether an isolated named database is required; that change must include the Go client and metadata together.
3. Confirm APIs, runtime service account, IAM scope, existing buckets/images, managed certificate domain, DNS owner, and retention. Project-wide datastore permission is not isolated database custody.
4. Obtain fresh explicit capacity authorization after this session's refusal, run integrated checks, and qualify the exact packaged image. Complete independent review and the existing merge gates.
5. Produce and review the actual preview with costs and deletion/replacement effects. Only then apply within the explicit approval. DNS changes follow configured Cloudflare MCP/repository IaC where applicable, never an ad hoc CLI bypass.
6. Keep `SMS_ENABLED=false`; Twilio campaign approval, retention, provider qualification, and live-send authority are separate. No phone-data collection is needed for online/PDF reports.

# is-optimized restart handover

This is a source checkpoint, not a new executable cloud task or deployment.
The coordinator should assign one continuation owner and retain the existing
registration, release history and infrastructure custody.

## Last productive activity before this request

2026-11-03 21:58:34 UTC: read-only diagnosis of the Twilio campaign rejection
and a request for the owner's choice between report-only delivery and a campaign
that also includes marketing. No consent change, resubmission or SMS followed.
The latest durable release journal predates that diagnosis.

## Source and acceptance

- Main checkpoint: `3cbbc162b8e5a054d639848783455b601600030e`.
- Existing implementation PRs #9 through #17 are merged. There was no open PR
  at handover inventory. Do not reopen or duplicate their completed lifecycle.
- Deployed server source: `4f9f983b4c2b1f857ebab46d3bcfc064b73473c3`.
  Subsequent main changes are CLI, documentation and a separate edge project;
  the deployed Go server source matches main's server implementation.
- CLI checkpoint: `50e98752b0c181399c4c1b78c3e78f706567a6c8`.
  Packed installation and public GitHub archive installation returned the same
  live JSON snapshot. Registry publication has not happened.
- Recorded acceptance includes CI race/vet/Linux build, CLI regression tests,
  public discovery/MCP, a fresh 170-finding report, JSON/latest/HTML/PDF snapshot
  consistency, and Nanos target boot. This is not a load benchmark.
- Health returned HTTP 200 during handover on 2026-11-04 UTC. This is a new
  health observation, not a new deployment or a refresh of all acceptance.

## Outstanding work and blockers

1. Twilio rejected the opt-in evidence because it includes marketing consent
   while the campaign describes non-marketing report delivery (error 30896).
   The owner must settle campaign scope before implementation changes. Align
   the real flow, description, samples, policies and screenshots; reconcile the
   existing registration identities and edit that registration. Do not create
   a duplicate campaign or assume approval. SMS remains disabled.
2. PR #17 contains the reviewed edge IaC change for Browser Integrity Check.
   That setting still blocks ordinary Python clients. The separate edge stack
   has no imported resources; native Cloudflare provider authentication is
   missing. No Cloudflare import, authenticated preview or apply has occurred.
   Preserve strict TLS, DNS, WAF, GCP and application admission controls.
3. Publishing the short npm package name needs npm authentication and publisher
   identity. The last authentication check returned `ENEEDAUTH`. Do not copy
   credential stores or infer publisher identity from this handover.

## Execution boundary

Cloud-eligible work includes Go/Node tests, deterministic scanner changes,
templates, documentation and Linux build/packaging qualification on an approved
runner. Preserve Go 1.26.0, OPS 0.1.47 and Nanos 0.1.56 pins. Requalify the
Linux OPS executable; do not reuse a Darwin executable on Linux. OPS file-cat
output adds one framing newline: remove exactly that byte before comparing
embedded CA file bytes, never strip arbitrary whitespace.

Signed-in browser work, screenshots of the actual opt-in flow and any Mac UI
or native acceptance stay local. Provider effects require the owning IaC,
approved credential custody and reviewed previews. A source checkpoint is not
provider authentication or authority for a new effect.

## Custody and resources

All owned implementation worktrees are committed. Private operational config
remains local. Three older uncommitted source copies and the original dirty
checkout are preserved separately; uniqueness has not been established and
they are not release inputs. Do not clean, reset, copy into a cloud checkout or
publish those files without reconciliation. Private journals, screenshots,
credentials and build artifacts are intentionally absent from this branch.

The existing production VM must keep running. This session holds no active
build lease and starts no replacement cloud owner, runner, restart or cleanup.
Local packaging jobs completed; historical artifacts remain preserved. Runtime,
provider and credential details are in the coordinator's private handover.

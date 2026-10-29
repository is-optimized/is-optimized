# Pilot cold-start recovery — 2026-10-29

The readiness implementation landed through PR2 after exact-source review, GitHub checks and a bounded Linux race/vet/static-build run. Its tree is `1778332c6c1310ba30abc587969ac251260ceec0`. Linux packaging pinned Go 1.26.0, OPS 0.1.47 and Nanos 0.1.56; archive SHA-256 is `f64bb83a422599a871e534424e1a72f36fe661cf55b3cf377ff48f9dbabbebc1`.

## Observed incident

Reviewed Pulumi plan changed three resources (archive, image and VM), leaving nineteen unchanged. Apply succeeded, but the new guest shut itself down before service acceptance. The old archive was restored through another reviewed Pulumi plan; that guest also shut down. GCP audit events identify `compute.instances.guestTerminate`, rather than external stop or preemption. The site was unavailable; an infrastructure apply was not service success.

Both archives subsequently booted and reached the Go application in isolated QEMU on the existing Linux host. Each stopped with `runtime metadata configuration unavailable` in that environment, which intentionally lacks the GCP metadata service. This proves kernel/application execution there, not the cause of GCP failure. Bootloader/kernel bytes and the transferred application hash were checked. Source POSIX executable bits are not serialized by OPS and are not established as causal.

PR3 added an authenticated GCE virtual display and explicit desired RUNNING state. Its reviewed in-place diagnostic start did not recover the service; screenshots were black or unavailable after guest exit, and GCE serial reads returned no console bytes.

## Bounded recovery changes

- Wait up to sixty seconds for transient runtime metadata availability, with per-attempt deadlines, bounded backoff and parent cancellation. Invalid JSON, unsupported configuration keys and invalid values fail without retry or partial environment mutation. Secret retrieval remains separate.
- Emit fixed startup-stage names and sanitized error categories without recording metadata values, credentials or provider response details.
- Include the documented Nanos `gcp` and `tls` kernel libraries plus a `gcp.logging` manifest tuple in GCP images. The unchanged images lacked that driver configuration, so granting a logging role alone could not have produced console logs.
- Grant the existing runtime identity `roles/logging.logWriter` in the existing project through IaC, with logging API ownership and an explicit VM dependency. No read permissions or new ingress are added. SMS remains disabled.

References: [Nanos GCP logging](https://github.com/nanovms/ops-documentation/blob/master/klibs.md#gcp-logging), [Pulumi instance properties](https://www.pulumi.com/registry/packages/gcp/api-docs/compute/instance/).

These are observability and bounded startup-hardening changes. The GCP cold-start root cause remains unconfirmed until target logs and runtime checks establish it. Keep source tests, local emulation, packaging, IaC apply and live acceptance separate. Do not declare recovery from RUNNING VM state alone.

## Custody and continuation

Use the existing `david-sire-run/is-optimized/pilot` stack in project `iono-run-512901`. Preserve Firestore, report/TLS buckets, stable origin address and retained images. The original archive and image remain rollback custody. Task receipts and logs live in the task-owned BuildOffload artifact directory; source does not contain credentials or console log payloads. Twilio application and screenshots remain pending authenticated browser access and a genuine opt-in flow; no SMS was sent.

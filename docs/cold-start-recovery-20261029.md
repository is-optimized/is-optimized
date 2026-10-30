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

## Verified recovery — 2026-10-30 UTC

PR4 landed as `513c41ed5ff1a1f64032f8a79261e833346ea55c`, with an identical source tree to reviewed and built candidate `b91784c3fc7d7d5d370cf1b8d96a70e7e76b94d2` (`859b4cf16d7edb0c1a93d86aa6ae82cbc0b9211c`). Targeted and aggregate Linux race tests, vet, and static Linux/amd64 build passed using Go 1.26.0. The exact packaged GCP image also passed an isolated Nanos/QEMU boot test with a metadata endpoint unavailable for twenty seconds, followed by health and homepage HTTP acceptance. That test used synthetic metadata and local storage; it did not establish GCP identity or TLS acceptance.

Recovery archive SHA-256: `4e6ec691a6f18c347c440cf7959c597b7a8542e808214839de7f78196340c27f`; ELF SHA-256: `97109d4b91c9432b18accfdb26b36ee0cfe1d626a3e65851ba8e850a6a9d0acc`. OPS 0.1.47 and Nanos 0.1.56 remained pinned. The reviewed recovery preview replaced only archive, image and VM; twenty-one resources stayed unchanged. Stack update 6 succeeded. Current image is `image-38c3515`.

GCP startup logs showed metadata, storage, HTTPS configuration, both listeners and both cached certificates available. The VM remained running beyond the earlier shutdown window. A real scan of example.com fetched one page and saved seven new zero-weight observations with methodology `2026-10-agentready-v1`, selection `home-links-document-order-v2` and task evaluation `not_run`.

- [Immutable example report](https://is-optimized.com/reports/32e3b1d373bf763081a723ae299cb4d51525) and its PDF returned HTTP 200; the latest-domain route resolved to the same report. HTML/PDF metadata and the PDF's rendered first page were checked.
- A sire.run robots request timed out. Its [saved report](https://is-optimized.com/reports/719aaa72c3b4d45fa97da717ac35e00553c9) correctly withheld scores and delivered HTML/PDF with unknown evidence. This is not proof that sire.run is generally unreachable.
- An independent read-only final check repeated health, homepage, immutable/latest HTML and PDF GETs, reconciled the live VM/image/stack outputs, and confirmed source-tree equality. It created no new scans or cloud effects.

The site is recovered, but the precise cause of the earlier cold-start shutdowns remains unproven. In the successful GCP boot metadata completed immediately, so do not claim the retry window alone caused recovery. Native console logging is now observable. Sustained load/capacity, browser interaction, Secret Manager/SMS delivery and Twilio application acceptance remain separate gates. All task-owned Linux workers were removed; no continuation daemon was installed.

# Agent readiness delivery — 2026-10-29

## Authority and source

Owner requested implementation of the reviewed recommendations, deployment as soon as possible, and headless implementation/review sessions. Owner subsequently requested completion of the existing Twilio application after signing in and authorized screenshots. These are separate effects: website deployment does not imply SMS provider approval or test-send authority.

Candidate branch: `feat/agentready-evidence-20261029`, based on release commit `27ad279`. The original `feat/mvp` checkout and existing release checkout contain active foreign changes and remain untouched. In particular, the separate 32-page sampling work is not imported wholesale into this release.

Acceptance: new immutable reports describe find/read/act signals, partial AgentReady references, observation level, applicability, and explicit absence of behavioral evaluation. HTML, PDF and coding briefs agree. Existing reports without metadata remain readable without invented observations. New informational checks do not penalize generic sites for absent auth, payment, MCP or NANDA capabilities. Existing bounded transport and scoring semantics remain intact.

The scanner/model and report/docs workers have disjoint file ownership in the isolated candidate checkout. The parent owns integration, aggregate verification, independent review and deployment effects. Native worker sessions inherit model/settings and have bounded deadlines; no external model runner or alternate provider is used.

## Existing deployment and rollback custody

Read-only reconciliation found GCP project `iono-run-512901`, zone `us-central1-a`, VM `is-optimized`, machine `e2-micro`, address `34.46.58.201`. Stack: `david-sire-run/is-optimized/pilot`. The owning IaC is the single-VM release topology with Cloudflare ingress and application ACME/TLS, not the root checkout's older MIG/load-balancer topology.

Prior image: `image-605a20a`. Prior archive: `/Volumes/BuildOffload/artifacts/is-optimized-release-20261028/image.tar.gz`, SHA-256 `ea36501ab8247b7ae6651cf86404cef947b76c210f22da96e0310b4a4bbe3702`. Keep both available. Prior image receipt identifies application source `080d7174b9bf5d9cc20e605e5ab7f43e8ea7c2ac`.

Packaging uses existing `scripts/package-nanos.sh`, Go 1.26.0, OPS 0.1.47 and Nanos 0.1.56. A replacement VM can interrupt the single-instance pilot. Review a saved Pulumi preview before applying; keep existing runtime identity, data, TLS cache and stable address. SMS remains disabled pending provider qualification.

## Twilio continuation

The existing draft in the original checkout records an approved brand, an existing messaging service and partially completed Low Volume Mixed wizard. It also records published policies at `https://sire.run/privacy/` and `https://sire.run/terms/`. These are historical draft records, not freshly observed console state. Resume and reconcile the existing wizard; do not register a duplicate.

The outstanding evidence is a publicly accessible opt-in screen or hosted screenshot of the actual proposed flow. A not-yet-live flow must be labeled accurately. Do not describe opening a message composer as sending or granting consent. Initial campaign scope is requested report-link replies; optional marketing permission is separate. Message samples, inbound correlation, HELP/STOP handling, policies and disclosure must match actual behavior before submission. No SMS has been sent by this delivery.

Browser connection attempts failed because the browser service runtime references an unavailable installed module. Ordinary script execution works, but authenticated browser access and screenshots are unavailable. No Twilio form fields were changed and no campaign submitted in this task. Restore the browser connection, obtain the owner's signed-in session, capture genuine evidence and continue the existing application. Screenshots have not yet been produced.

Official reference: [Twilio message-flow requirements](https://www.twilio.com/docs/api/errors/30909), checked 2026-10-29, requires an accurate complete workflow and public evidence for non-public opt-in flows. Verify message purpose, frequency, rates, STOP/HELP and policy links in the delivery flow itself. The existing optional marketing disclosure includes other communication channels; do not represent it as an independently qualified SMS opt-in. This records provider instructions, not legal qualification.

## Verification and release evidence

`gofmt` and `git diff --check` passed for the authored source. Tests, build, packaging and deployment are pending. The bounded host-admission sample held heavy execution: load 7.27, CPU idle 81.2%/80.61%, normal-memory condition false, swap rates 26,154 and 2,849,909 bytes/second, no thermal/performance warning. Receipt: `/Volumes/BuildOffload/artifacts/is-optimized-agentready-20261029/admission.json`. No automatic capacity polling, heavy test/build or fallback host was started after refusal. Source review and a draft PR may proceed without claiming executable acceptance.

Record independent review disposition, immutable archive receipt, reviewed preview, apply result and live scan/report/PDF behavior separately. Source changes and local tests do not establish deployment or provider acceptance.

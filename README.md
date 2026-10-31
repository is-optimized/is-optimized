# is-optimized.com

A Go website-readiness audit and shareable HTML/PDF report service by Sire Run, Inc.

## Development

Go 1.26 is required. With no cloud settings, `go run ./cmd/server` uses a private `.data` directory and listens at `http://localhost:8080`. This local store is single-process development storage, not an HA substitute for Firestore. SMS is disabled by default.

Submit a domain in the browser. The application examines its homepage and up to 31 additional same-site pages selected from internal links and bounded sitemap discovery, with a two-hop link limit and an SSRF-safe fetcher. Reports include SEO, GEO, and AI-agent readiness evidence, remediation, and coding-agent instructions. Findings identify find/read/act stages, applicability, evidence levels, and partial AgentReady reference mappings. New interface-discovery observations are informational; linked interfaces are not validated, and no agent task is executed. Read [the methodology](docs/methodology.md) before interpreting scores.

When execution capacity is admitted, run `go test -race ./...`, `go vet ./...`, and `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/server ./cmd/server`. These commands are verification instructions, not claims of successful execution in this delivery.

## Configuration

| Variable | Purpose |
| --- | --- |
| `PUBLIC_URL` | Canonical origin, no trailing path; HTTPS required for SMS |
| `LISTEN_ADDR` | Default `:8080`; place behind trusted TLS ingress in production |
| `DATA_DIR` | Local development directory; default `.data` |
| `GCP_PROJECT_ID`, `GCS_REPORT_BUCKET` | Enable Firestore and private GCS artifacts using application-default credentials |
| `TRUSTED_PROXY_CIDRS` | Explicit ingress ranges; never trust arbitrary client forwarding headers |
| `SMS_ENABLED` | `true` only after provider configuration and disclosure acceptance |
| `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM` | Runtime-only provider configuration; never place tokens in images or source |
| `TWILIO_AUTH_TOKEN_SECRET` | Pinned Secret Manager version resource; production token retrieved through ADC |
| `CONFIG_FROM_METADATA` | Set true in Nanos image to load Pulumi-managed nonsecret instance configuration |
| `CONSENT_DISCLOSURE`, `CONSENT_VERSION` | Owner-supplied wording and immutable version identifier |

Twilio inbound URL: `/webhooks/twilio/inbound`; status callback URLs are generated per request. Signatures are checked against `PUBLIC_URL` plus the exact request URI. Configure provider opt-out support. A report request and optional marketing choice are separate data. This service sends report links only; it does not authorize or perform marketing campaigns or calls.

## Deployment and operations

Read [operations](docs/operations.md), [RFC](docs/rfc/001-mvp.md), and [delivery evidence](docs/devlog.md). Infrastructure is defined in `infra/Pulumi.yaml`; it has not been previewed or applied. Never use OPS cloud create commands to bypass the IaC boundary. A Linux binary or synthetic Twilio test is not Nanos/GCP/provider qualification.

The hosted `is-agentic` scanner source was unavailable. This implementation uses an independent documented methodology, not a source port or demonstrated scoring parity.

## Brand and current acceptance

The embedded editorial UI and report share [brand guidance](docs/brand.md). Current design/deployment work is recorded in [verification](docs/verification.md) and the [costed deployment handoff](docs/deployment-candidate.md). SMS remains disabled pending its separate approval and qualification.

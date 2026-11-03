# Public programmatic access

This separate Pulumi YAML project manages exactly one existing setting for
`is-optimized.com`: Browser Integrity Check (`browser_check`) is `off`. It does
not manage DNS, SSL, WAF, DDoS settings, application admission controls, or GCP.
The provider is pinned to the locally verified Cloudflare `6.20.0` schema.

## Reason and observed state

Read-only checks on 2026-11-03 found the zone's Browser Integrity Check enabled.
The default Python `urllib` user agent received Cloudflare HTTP 403, including
`error code: 1010`, while Go, Node, curl and an explicit verification user agent
reached the healthy origin. Python `urllib` was also blocked from `llms.txt`,
the skills index, the public report API, and the MCP route. A custom user agent
was a verification workaround; it did not establish access for ordinary clients.

[Cloudflare documents error 1010 as Browser Integrity Check](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-1xxx-errors/error-1010/).
This public service intentionally accepts legitimate browser and programmatic
clients, so the zone-level browser heuristic is disabled. Other security
settings and application rate limits remain under their existing owners.

`dns-cutover.yaml` is historical reviewed intent, not executable IaC or a current
receipt. In particular, the live SSL setting was read as `strict`; its old
`full` entry must not be replayed as part of this change.

## Credential custody and adoption

Source preparation does not initialize a backend stack, import state, preview
authenticated provider behavior, or change Cloudflare. The coordinator must
verify the approved backend and create/select a stack for `is-optimized-edge`
before the steps below. The existing GCP `is-optimized/pilot` stack is separate.

Provide a Cloudflare API token through the provider's native
`CLOUDFLARE_API_TOKEN` environment variable in an approved executor, or enter it
interactively into encrypted Pulumi configuration:

```sh
pulumi config set --secret cloudflare:apiToken
```

Do not supply a token as a shell argument, paste it into chat, commit it, copy
credentials from the MCP connector, or persist it in unencrypted state. The
verified provider schema marks `apiToken` secret. Required token permission is
**Zone → Zone Settings → Edit**, scoped to the specific `is-optimized.com` zone.
No DNS-edit or account-wide permissions are needed for this resource.

Set the nonsecret zone ID using `config.example.yaml` as the reference. All
following commands run from this directory against the separately selected
edge stack; verify the stack with `pulumi stack` first.

```sh
pulumi config set zoneId c110deebc79d486a28722cca21255430
pulumi import cloudflare:index/zoneSetting:ZoneSetting browserIntegrityCheck \
  c110deebc79d486a28722cca21255430/browser_check \
  --version 6.20.0 --generate-code=false --protect
pulumi preview --diff
```

The [upstream resource's import contract](https://github.com/cloudflare/terraform-provider-cloudflare/blob/main/docs/resources/zone_setting.md#import)
is `<zone_id>/<setting_id>`. Import adopts the existing setting; it does not
create or recreate the zone. Import it before applying the desired `off` value.
Review must show exactly this setting changing from `on` to `off`, plus only the
necessary stack/provider bookkeeping. Any DNS, SSL, WAF, zone replacement,
deletion, unexpected resource, or credential exposure stops the apply.

After independent review and a successful authenticated preview, the coordinator
may apply through this project. Read the setting back and verify default Python,
Go, Node, and curl clients against health, public HTML, discovery files, report
API and MCP initialization/list operations. Preserve exact source, preview,
provider result and endpoint receipts separately; source/schema validation is
not provider acceptance. Keep reports public without sending SMS or submitting
new scans merely to verify the setting.

## Rollback

Change only this resource's `value` back to the string `"on"` in a reviewed
source revision, preview the single-setting update, and apply through this
project. Read the value back. This restores BIC's user-agent blocking behavior.
Do not delete the resource or destroy the stack as a rollback; deletion is
protected and the setting is retained.

## Source checks and current blocker

```sh
go test ./infra/cloudflare -count=1
pulumi package get-schema cloudflare@6.20.0
```

The focused test checks the actual YAML resource boundary, provider version,
required zone configuration, string value, protection, and example zone against
the checked-in ownership record. The installed provider schema was read locally:
`cloudflare:index/zoneSetting:ZoneSetting` requires `zoneId`, `settingId`, and
`value`; its `apiToken` input is secret. No provider calls are required for those
checks. A provider plugin alone is not a credential grant.

At preparation, this session had no Cloudflare provider credential in the native
environment or the existing GCP stack configuration, and that stack had no
Cloudflare resources/provider. The connected MCP session could read the zone,
but its credentials were not exported. Therefore authenticated edge-stack
preview/adoption/apply remain blocked on an approved provider credential route
and coordinator-owned stack initialization. No backend stack was initialized.

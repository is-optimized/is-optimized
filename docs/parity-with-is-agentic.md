# Is-agentic parity inventory

Status: draft inventory. This file is the working map for the parity effort
requested in `~/Downloads/is-optimized-coding-agent-prompt.md`. It is not a
claim of full parity or live verification. The supplied prompt summarizes
observations from `prosody.chat`; the seven underlying reports and the complete
`npx is-agentic` inventory are not yet in the repository.

## Existing checks in the current scanner

These IDs are registered in `internal/scanner/checks.go` or emitted by the
scanner's observation pipeline. They inspect bounded initial HTML and response
metadata, unless the evidence row says otherwise.

| Is-agentic topic from the supplied prompt | Is-optimized check ID | Current coverage and limit |
| --- | --- | --- |
| Public page response | `seo.access`, `geo.access`, `agent.access` | Weighted per sampled page; HTTP/initial HTML only. |
| Index directives | `seo.index` | Weighted; robots meta/header on fetched pages. Does not yet evaluate all AI crawler groups. |
| Page title and duplicate titles | `seo.title` | Weighted; duplicate comparison across distinct sampled final URLs. |
| Search description and duplicate descriptions | `seo.description` | Weighted; duplicate comparison across distinct sampled final URLs. |
| Canonical URL | `seo.canonical` | Weighted; declared canonical checks only; target reachability and cross-page consistency are separate gaps. |
| Heading structure | `seo.headings`, `geo.structure` | Weighted; H1 and skipped-level heuristics on sampled HTML. |
| Mobile viewport | `seo.viewport` | Weighted declaration check; no rendered mobile-layout test. |
| Image alternatives | `seo.images` | Weighted; alt presence heuristic, not image understanding. |
| Structured entity data | `geo.entity` | Weighted JSON-LD presence/type heuristic, not full schema validation or business truth. |
| Semantic structure | `geo.structure` | Weighted heading/list/table signals. |
| Article attribution | `geo.author` | Weighted heuristic. Prompt identifies false positives from `<article>`; validate/fix this before widening scoring. |
| Navigation landmarks | `agent.landmarks` | Weighted `main`/`nav` presence heuristic. |
| Named controls and links | `agent.controls`, `agent.links` | Weighted accessible-name heuristics; no browser accessibility tree. |
| Sitemap declaration | `seo.sitemap` | Informational sitemap discovery evidence; sitemap quality and freshness scoring are not implemented. |
| HSTS and initial HTML word count | `seo.hsts`, `seo.words` | Informational only in the current methodology. |
| Sampled page outcome | `seo.page_status` | Informational requested/final URL and status evidence. |
| Linked docs and pricing | `agent.find.docs`, `agent.read.pricing` | Zero-weight discovery observations; links are not fetched or validated. |
| Linked machine-readable resources | `agent.find.resources` | Zero-weight discovery observation; presence is not contract validation. |
| Declared API/auth/agent interfaces | `agent.act.openapi`, `agent.act.auth`, `agent.act.interfaces` | Zero-weight link discovery; no API, OAuth, MCP, or task execution. |
| Structured facts vs initial text | `agent.read.facts` | Zero-weight exact-text observation with bounded JSON-LD extraction; not truth/semantic validation. |

## Requested parity additions and current mapping state

Every row below is an explicit work item from the supplied prompt. “Pending” is
deliberate: an existing related heuristic is not treated as equivalent coverage.

| Is-agentic topic | Planned stable check ID | Existing mapping | State / boundary |
| --- | --- | --- | --- |
| Robots directives for named AI crawlers and `Content-Signal` | `agent.robots` | `seo.index` (partial) | Pending; distinguish retrieval from training policy and show per-bot unspecified/allowed/blocked. |
| Retrieval-agent vs browser response parity | `agent.retrieval_access` | `agent.access` (partial) | Pending; bounded read-only GET probes; compare status and body length. |
| Sitemap validity, same-host URLs, sample freshness | `seo.sitemap_quality` | `seo.sitemap` (partial) | Pending; existing bounded discovery is not full quality coverage. |
| Markdown negotiation, fallback files, alternate links and format quality | `agent.markdown` | None | Pending; HTML behavior must remain intact and `Vary: Accept` must be observed. |
| HTML and Markdown error routes | `agent.markdown_404` | None | Pending; random-path status and body evidence. |
| Refresh/script redirect stubs, chains and host/scheme consistency | `seo.redirect_hygiene` | `seo.page_status` (partial) | Pending; redirect behavior must be observed, not inferred from markup alone. |
| Open Graph, Twitter card, language and resolved image | `seo.social_metadata` | `seo.canonical` (partial) | Pending; external image fetches need the shared SSRF-safe budget. |
| About/contact/privacy/terms trust pages | `geo.trust_pages` | `agent.find.docs` (partial) | Pending; probe a fixed bounded path set and score proportional availability/content. |
| Organization identity completeness and sameAs | `geo.organization` | `geo.entity` (partial) | Pending; parse JSON-LD types/fields without asserting truth. |
| Main-text token estimate and visible-text/HTML ratio | `agent.content_budget`, `geo.content_ratio` | `agent.read.facts` (partial) | Pending; same parser/text extraction with explicit estimate limits. |
| OpenAPI discovery and operation/schema quality | `agent.openapi.*` | `agent.act.openapi` (discovery only) | Pending; only read-only GET of advertised or standard paths; parser and bounds required. |
| Live unauthenticated API behavior, REST/GraphQL, rate limits and error shape | `agent.api.*` | `agent.act.openapi` (discovery only) | Pending; only safe unauthenticated GET; never invoke mutations. |
| OAuth/OIDC/protected-resource discovery and PKCE | `agent.oauth.*` | `agent.act.auth` (discovery only) | Pending; validate public metadata only, no registration or token exchange. |
| MCP discovery, read-only initialization/list methods, schemas, annotations and errors | `agent.mcp.*` | `agent.act.interfaces` (discovery only) | Pending; strictly restrict calls to the read-only methods specified by the prompt. |
| MCP registry/card/apps metadata | `agent.mcp_registry.*` | None | Pending; registry listing is off-site and may require a supported public source. |
| Developer portal, SDK, CLI, sandbox, pricing and GitHub linkage | `agent.developer_surfaces.*` | `agent.find.docs`, `agent.read.pricing` (partial) | Pending; distinguish first-party observed links from off-site registry claims. |
| Quality and modularity of llms/agent files | `agent.instructions.*` | `agent.find.resources` (discovery only) | Pending; validate bounded Markdown/JSON and sampled same-origin links. |
| Commerce profile and Product/Offer signals | `agent.commerce.*` | `geo.entity` (partial) | Pending; applicability depends on site type; no checkout or purchase actions. |
| Search/entity discoverability | `agent.discoverability.*` | None | Pending; search-key-dependent checks remain not observed when unavailable. |
| Accessible controls, labels and landmarks | `agent.accessibility.*` | `agent.controls`, `agent.links`, `agent.landmarks` (partial) | Pending; keep static HTML evidence limits explicit. |
| Hidden prompt-instruction indicators | `agent.prompt_injection_signals` | None | Pending; detection reports evidence only and never follows or executes text. |
| Site-type declaration and applicable check selection | `site.type` | Existing per-check applicability | Pending; extend the existing mechanism instead of introducing a parallel one. |
| Optional observed task evaluation | `agent.task_evaluation` | `Report.BehavioralEvaluation` | `not_run` by default; opt-in, off-score, and needs separate cost/credential authorization. |

## Shared scoring and evidence work

The current public score model has SEO, GEO, and AI-agent readiness pillars.
Pillar scores are withheld below 80% observed coverage or when foundational
evidence is unknown. Optional interface discovery currently has zero weight.
The requested work must decide which checks are Essential, Recommended, or
Bonus; preserve `unknown` vs `not_applicable`; avoid rewarding unobserved
weight-zero checks; and make one computed result feed the JSON API, HTML/PDF,
and developer brief. The proposed fourth/agent-readiness score and its
applicability-aware denominator are not implemented by this inventory commit.

All new findings must keep stable IDs, pillar/stage, weight and applicability,
proportional verdicts, bounded evidence, remediation examples, and any
defensible specification references. Scanned content remains untrusted input.

## Verification still required

- Capture the complete check inventory and baseline scores from the seven
  public reports plus fresh `npx is-agentic` output for the requested sites.
- Add one matrix row per externally reported check and resolve every unmapped
  row as implemented, justified not applicable, or explicitly unsupported.
- Add pass/partial/fail/not-applicable fixtures and an integration-site fixture
  for each implementation section.
- Compare scanner verdicts, API, HTML/PDF/developer brief output, and live
  evidence; record disagreements and scan timings before and after.
- Never present a probe as passed or failed when it was blocked, timed out, or
  could not be verified.

# Is-agentic parity inventory

Status: draft inventory. This file is the working map for the parity effort
requested in `~/Downloads/is-optimized-coding-agent-prompt.md`. It is not a
claim of full parity. Fresh public `npx is-agentic@1.0.1` JSON reports were
collected for the eight requested domains on 2026-11-02 UTC. The CLI returns
failed and partial issues, but omits its passing-check inventory, so these
reports do not disclose every check behind its score.

## Captured public report baselines

| Domain | Is Agentic score | Report |
| --- | ---: | --- |
| `prosody.chat` | 68 | [report](https://is-agentic.com/scan/prosody.chat) |
| `is-agentic.com` | 100 | [report](https://is-agentic.com/scan/is-agentic.com) |
| `vercel.com` | 96 | [report](https://is-agentic.com/scan/vercel.com) |
| `github.com` | 69 | [report](https://is-agentic.com/scan/github.com) |
| `galileo.ai` | 98 | [report](https://is-agentic.com/scan/galileo.ai) |
| `gymshark.com` | 37 | [report](https://is-agentic.com/scan/gymshark.com) |
| `parallel.ai` | 94 | [report](https://is-agentic.com/scan/parallel.ai) |
| `is-optimized.com` | 68 | [report](https://is-agentic.com/scan/is-optimized.com) |

These are mutable public report URLs and may be refreshed upstream. The exact
retrieved JSON was kept in the local task's temporary directory, not committed.
The CLI reported 29 distinct failed or partial issue IDs across the eight
reports. Every observed issue is mapped below; issue-only output is not enough
to claim that the upstream inventory is complete.

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
| Article attribution | `geo.author` | Weighted; applicability now requires Article/BlogPosting/NewsArticle JSON-LD or `og:type=article`; an HTML `<article>` wrapper alone is not enough. |
| Navigation landmarks | `agent.landmarks` | Weighted `main`/`nav` presence heuristic. |
| Named controls and links | `agent.controls`, `agent.links` | Weighted accessible-name heuristics; no browser accessibility tree. |
| Sitemap declaration | `seo.sitemap_quality`, `seo.sitemap_freshness` | Bounded XML validation, same-origin candidate collection, and first-500 `lastmod` sample; no full sitemap certification. |
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
| Robots directives for named AI crawlers and `Content-Signal` | `agent.robots` | `seo.index` (partial) | Implemented partially: reports specified/unspecified rules and Content-Signal, plus four retrieval-UA status/body-size probes; challenge detection is heuristic and policy matrix is not full protocol certification. |
| Retrieval-agent vs browser response parity | `agent.retrieval_access` | `agent.access` (partial) | Pending; bounded read-only GET probes; compare status and body length. |
| Sitemap validity, same-host URLs, sample freshness | `seo.sitemap_quality`, `seo.sitemap_freshness` | `seo.sitemap` (partial) | Implemented with robots-declared then conventional-path fallbacks; up to five sitemap documents and 5,000 candidate URLs, freshness sampled over up to 500 entries. |
| Markdown negotiation, fallback files, alternate links and format quality | `agent.markdown` | None | Implemented partially: homepage Accept probe, Vary check, `/index.md` fallback; frontmatter, code fences, and alternate-link target validation remain. |
| HTML and Markdown error routes | `agent.markdown_404` | None | Implemented: unique-path HTML/Markdown GET probes; validates 404/410, Markdown media type, minimum body length, and link syntax, but not link destination. |
| Refresh/script redirect stubs, chains and host/scheme consistency | `seo.redirect_hygiene` | `seo.page_status` (partial) | Implemented partially: checks meta refresh, short-page location stubs, recorded chain length, and HTTPS downgrade; no final cross-page apex/www consistency analysis. |
| Open Graph, Twitter card, language and resolved image | `seo.social_metadata` | `seo.canonical` (partial) | Implemented for eight homepage signals; image is fetched through the SSRF-safe shared budget; no visual preview-quality judgment. |
| About/contact/privacy/terms trust pages | `geo.trust.*` | `agent.find.docs` (partial) | Implemented partially: probes four same-origin pages and checks 500 visible-text bytes; one standard route per topic, no legal/claim assessment. |
| Organization identity completeness and sameAs | `geo.organization`, `geo.entity_links`, `geo.schema_breadth` | `geo.entity` (partial) | Implemented partially: checks supported entity fields, sameAs count, and capped type-breadth bonus; does not prove entity ownership or claim accuracy. |
| Main-text token estimate and visible-text/HTML ratio | `agent.content_budget`, `geo.content_ratio` | `agent.read.facts` (partial) | Implemented: tokenizer-independent text estimate and visible-text/HTML byte ratio with explicit thresholds; candidate live-site evidence remains. |
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

## Atomic mapping for every issue returned by the captured reports

The upstream CLI's JSON schema exposes issue IDs and score totals, not passing
check definitions. This is therefore an exhaustive mapping of returned issues,
not of all upstream checks. A planned ID identifies implementation work; it
does not imply that the check already exists.

| Upstream issue ID | Tier | Reports with failed/partial issue | Is-optimized mapping | Mapping status |
| --- | --- | --- | --- | --- |
| `agent-friendly-404` | Essential | prosody.chat, github.com, gymshark.com, is-optimized.com | `agent.markdown_404` | Implemented site-level response probe; pending candidate live-site verification. |
| `agent-instruction` | Recommended | prosody.chat, gymshark.com, parallel.ai, is-optimized.com | `agent.instructions.when_to_use` | Existing link discovery is insufficient; content-quality check planned. |
| `agentic-search-specific` | Recommended | prosody.chat, gymshark.com, parallel.ai | `agent.discoverability.docs` | Search-provider/key dependent; not observed without a configured source. |
| `api-error-model` | Recommended | github.com | `agent.openapi.error_schema` | Planned contract parsing. |
| `api-schema-analysis` | Recommended | github.com, gymshark.com | `agent.openapi.complexity` | Planned bounded schema analysis. |
| `api-versioning-policy` | Recommended | github.com | `agent.openapi.versioning` | Planned explicit version/deprecation evidence. |
| `brand-search-accuracy` | Recommended | prosody.chat, is-agentic.com, vercel.com, galileo.ai, is-optimized.com | `agent.discoverability.brand` | Search-provider/key dependent; never infer from page HTML. |
| `cli-tool` | Recommended | galileo.ai, gymshark.com | `agent.developer.cli` | Planned first-party package discovery. |
| `content-no-js` | Essential | vercel.com, github.com, galileo.ai, gymshark.com, parallel.ai | `agent.access` | Existing initial-HTML heuristic is partial; no browser execution. |
| `developer-portal` | Recommended | gymshark.com | `agent.developer.portal` | Existing docs-link heuristic is partial. |
| `function-calling-compat` | Recommended | github.com, gymshark.com | `agent.openapi.function_calling` | Planned typed operation/schema analysis. |
| `json-error-responses` | Essential | gymshark.com | `agent.api.json_errors` | Planned; only unauthenticated GET probes. |
| `json-ld` | Recommended | prosody.chat, github.com, gymshark.com, is-optimized.com | `geo.entity` | Existing entity-presence heuristic is partial; validation/completeness planned. |
| `markdown-negotiation-vary` | Essential | prosody.chat, github.com, gymshark.com, is-optimized.com | `agent.markdown` | Planned server-side GET negotiation probe. |
| `mcp-resource-listing` | Essential | gymshark.com, parallel.ai | `agent.mcp.resources` | Planned read-only MCP discovery/list calls. |
| `mcp-server` | Recommended | vercel.com, github.com | `agent.mcp.server` | Existing interface-link observation is partial. |
| `metadata-completeness` | Recommended | prosody.chat, gymshark.com, is-optimized.com | `seo.social_metadata` | Implemented for sampled homepage metadata; candidate scan evidence remains to be collected. |
| `oauth-support` | Essential | parallel.ai | `agent.oauth.metadata` | Existing auth-link observation is partial; metadata-only validation planned. |
| `onboarding-friction` | Recommended | vercel.com, gymshark.com | `agent.api.onboarding` | Planned public health/error/onboarding observations. |
| `openapi-spec` | Essential | github.com, gymshark.com | `agent.openapi.discovery` | Existing link discovery is partial; document parsing planned. |
| `org-schema-completeness` | Recommended | prosody.chat, github.com, galileo.ai, gymshark.com, parallel.ai, is-optimized.com | `geo.organization` | Existing entity-presence check is partial; field/relationship validation planned. |
| `pricing-info` | Recommended | gymshark.com | `agent.read.pricing` | Existing linked-pricing observation is partial; no pricing policy inferred. |
| `public-api` | Recommended | galileo.ai, gymshark.com | `agent.api.reachability` | Planned read-only advertised GET probe. |
| `public-api-docs` | Recommended | gymshark.com | `agent.developer.api_links` | Existing docs/resource links are partial. |
| `rate-limit-headers` | Recommended | vercel.com, github.com, galileo.ai, parallel.ai | `agent.api.rate_limits` | Planned; probe safe unauthenticated GET responses only. |
| `redirect-hygiene` | Essential | gymshark.com | `seo.redirect_hygiene` | Existing requested/final URL evidence is partial; chain/markup checks planned. |
| `scoped-permissions` | Essential | vercel.com, gymshark.com | `agent.openapi.scopes` | Planned scope parsing; no authorization or token exchange. |
| `sitemap` | Recommended | prosody.chat, is-optimized.com | `seo.sitemap_quality` | Implemented bounded validity and same-origin check; candidate live-site evidence remains. |
| `trust-anchors` | Recommended | prosody.chat, galileo.ai, gymshark.com, is-optimized.com | `geo.trust_pages` | Planned bounded probes for about/contact/privacy/terms. |

Captured Is-Optimized production baselines use the older `2026-10-agentready-v1`
methodology, despite the current source being newer. The public service returned
these completed reports: `prosody.chat` SEO 95.8 / GEO 70 / AEO 95.8 (2 pages),
`is-agentic.com` 100 / 93.4 / 100 (5 pages), and `vercel.com` 94.2 / 93.1 / 92.9
(5 pages). The `github.com` report had only one page and all scores withheld;
`galileo.ai` was still running at last read. Further POST scan requests were
rate-limited, so no retry was made. These are production baselines, not
verification of this candidate.

## Shared scoring and evidence work

The existing SEO, GEO, and AEO scores remain separate. This branch adds an
additive Agent readiness score with an 80-point Essential pool, 20-point
Recommended pool, and non-penalty Bonus capped at +5 (final score capped at
100). Unknown results reduce coverage but are not counted as failures; not
applicable results leave the denominator. The score is withheld below 80%
coverage or until both scored tiers have observed findings. Current weighted
page checks now carry Essential/Recommended tiers and feed this score; site
level P0 checks, per-tier calibration, and live candidate comparisons remain
in progress.

All new findings must keep stable IDs, pillar/stage, weight and applicability,
proportional verdicts, bounded evidence, remediation examples, and any
defensible specification references. Scanned content remains untrusted input.

## Verification still required

- Initial implementation section: correct the `geo.author` applicability false positive. Fixture coverage was added for an HTML-only `<article>` container, `og:type=article`, BlogPosting JSON-LD, and a NewsArticle JSON-LD graph. Source and fixture are committed in the next checkpoint; CI and live-site checks remain pending.
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

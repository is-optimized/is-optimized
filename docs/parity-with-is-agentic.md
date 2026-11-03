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
| Retrieval-agent vs browser response parity | `agent.robots` | `agent.access` (partial) | Implemented inside the named crawler policy check: compares four retrieval user agents against the normal homepage status and response byte length; not a semantic body comparison. |
| Sitemap validity, same-host URLs, sample freshness | `seo.sitemap_quality`, `seo.sitemap_freshness` | `seo.sitemap` (partial) | Implemented with robots-declared then conventional-path fallbacks; up to five sitemap documents and 5,000 candidate URLs, freshness sampled over up to 500 entries. |
| Markdown negotiation, fallback files, alternate links and format quality | `agent.markdown` | None | Implemented partially: homepage Accept probe, `Vary` check, HTML response and `/index.md` fallback; page-specific `.md` routes, alternate-link target validation, frontmatter and code-fence validation remain. |
| HTML and Markdown error routes | `agent.markdown_404` | None | Implemented: unique-path HTML/Markdown GET probes; validates 404/410, Markdown media type, minimum body length, and link syntax, but not link destination. |
| Refresh/script redirect stubs, chains and host/scheme consistency | `seo.redirect_hygiene` | `seo.page_status` (partial) | Implemented partially: checks meta refresh, short-page location stubs, recorded chain length, and HTTPS downgrade; no final cross-page apex/www consistency analysis. |
| Open Graph, Twitter card, language and resolved image | `seo.social_metadata` | `seo.canonical` (partial) | Implemented for eight homepage signals; image is fetched through the SSRF-safe shared budget; no visual preview-quality judgment. |
| About/contact/privacy/terms trust pages | `geo.trust.about`, `geo.trust.contact`, `geo.trust.privacy`, `geo.trust.terms` | `agent.find.docs` (partial) | Implemented partially: probes four same-origin pages and checks 500 visible-text bytes; one standard route per topic, no legal/claim assessment. |
| Organization identity completeness and sameAs | `geo.organization`, `geo.entity_links`, `geo.schema_breadth` | `geo.entity` (partial) | Implemented partially: checks supported entity fields, sameAs count, and capped type-breadth bonus; does not prove entity ownership or claim accuracy. |
| Main-text token estimate and visible-text/HTML ratio | `agent.content_budget`, `geo.content_ratio` | `agent.read.facts` (partial) | Implemented: tokenizer-independent text estimate and visible-text/HTML byte ratio with explicit thresholds; candidate live-site evidence remains. |
| OpenAPI discovery and operation/schema quality | `agent.openapi.spec`, `agent.openapi.operations`, `agent.openapi.errors`, `agent.openapi.scopes`, `agent.openapi.versioning`, `agent.openapi.function_calling`, `agent.openapi.schema_complexity`, `agent.openapi.patterns` | `agent.act.openapi` (discovery only) | Implemented partially: follows at most three public JSON/YAML candidates when API/developer links are present; measures OpenAPI 3 root, operation identifiers/descriptions, typed responses, declared scopes/version, JSON tool-call signals, schema node complexity, pagination, idempotency, async 202 and batch patterns. All are bounded static heuristics; no operation is executed unless it is one of the selected safe unauthenticated GET probes below. |
| Live unauthenticated API behavior, REST/GraphQL, rate limits and error shape | `agent.api.reachability`, `agent.api.json_errors`, `agent.api.rate_limits`, `agent.api.onboarding` | `agent.act.openapi` (discovery only) | Implemented partially: selects at most two parameter-free public GET operations from OpenAPI servers and checks observed JSON response, errors, rate headers and zero-auth health/status behavior. No credentials, mutations or GraphQL introspection; JSON error shape remains unknown when probes do not produce an error. |
| RFC 9727 well-known API catalog and Linkset endpoint discovery | `agent.api_catalog` | `agent.act.openapi` (discovery only) | Implemented: when an API/developer surface is advertised, requests `/.well-known/api-catalog` with `Accept: application/linkset+json` and checks for valid Linkset endpoint references. Does not call those endpoints. |
| OAuth/OIDC/protected-resource discovery and PKCE | `agent.oauth.discovery`, `agent.oauth.pkce`, `agent.oauth.scopes`, `agent.oauth.protected_resource` | `agent.act.auth` (discovery only) | Implemented partially: reads RFC 8414/OIDC and RFC 9728 metadata for linked auth/API surfaces; checks issuer, authorization endpoint, S256, scopes, and resource server URLs. No client registration, token exchange, or live authorization flow. |
| MCP discovery, read-only initialization/list methods, schemas, annotations and errors | `agent.mcp.*` | `agent.act.interfaces` (discovery only) | Implemented bounded Streamable HTTP initialization, negotiated lists, metadata and one malformed list probe. No tools are called; resource contents and Apps HTML/CSP remain explicitly unobserved outside the allowed methods. |
| MCP registry/card/apps metadata | `agent.mcp_registry.*` | None | Server-card discovery and Apps descriptor metadata implemented. Registry membership is explicitly unknown because no supported registry lookup is configured; an advertised link is not registration proof. |
| Developer portal, SDK, CLI, sandbox, pricing and GitHub linkage | `agent.developer.portal`, `agent.developer.docs`, `agent.developer.sdk`, `agent.developer.cli`, `agent.developer.sandbox`, `agent.developer.github`, existing `agent.read.pricing` | `agent.find.docs`, `agent.read.pricing` | Implemented as bounded homepage-link observations when a developer/API surface is applicable. External package links are not fetched or attributed to the site; presence does not certify registry ownership, product support or sandbox behavior. |
| Quality and modularity of llms/agent files | `agent.instructions.quality`, `agent.instructions.when_to_use`, `agent.instructions.full`, `agent.instructions.agents_md`, `agent.resource.skills_index`, `agent.resource.auth_guide` | `agent.find.resources` (discovery only) | Implemented partially: llms files, AGENTS.md/agents.md presence, Agent Skills Discovery v0.2.0 index and sampled raw SHA-256 artifact checks, and auth.md topic coverage. AI catalog and MCP server card checks are handled in the separate MCP surface implementation; full instruction quality/modularity is not assessed. |
| Commerce profile and Product/Offer signals | `agent.commerce.profile`, `agent.commerce.checkout` | `geo.entity` (partial) | Implemented for inferred or explicitly declared stores using homepage Product/Offer schema and product/pricing/cart/checkout links. The checkout signal is informational; no checkout page or purchase action is performed. |
| Search/entity discoverability | `agent.discoverability.*` | None | Explicit unknown findings for external brand/docs/entity/directory observations; supported search-provider integration remains a credential-dependent exclusion. No ranking is inferred from HTML. |
| Accessible controls, labels and landmarks | `agent.form_labels`, `agent.controls`, `agent.links`, `agent.landmarks` | Current static HTML checks count label associations, named controls and links, and main/nav landmarks. No browser accessibility tree or interaction testing. |
| Hidden prompt-instruction indicators | `agent.prompt_injection_signals` | None | Implemented as an informational-only pattern signal across hidden text and selected accessibility attributes; matched text is omitted from evidence and never followed or executed. |
| Site-type declaration and applicable check selection | `site.type` | Existing per-check applicability | Implemented as an informational override/inference (`docs`, `business`, `app`, `store`). It informs developer checks and gates storefront scoring, but does not globally suppress unrelated checks by type. |
| Optional observed task evaluation | `agent.task_evaluation` | `Report.BehavioralEvaluation` | Explicitly excluded by owner: no paid LLM or simulated agent task runs. `behavioral_evaluation` remains `not_run`. |

## Atomic mapping for every issue returned by the captured reports

The upstream CLI's JSON schema exposes issue IDs and score totals, not passing
check definitions. This is therefore an exhaustive mapping of returned issues,
not of all upstream checks. A planned ID identifies implementation work; it
does not imply that the check already exists.

| Upstream issue ID | Tier | Reports with failed/partial issue | Is-optimized mapping | Mapping status |
| --- | --- | --- | --- | --- |
| `agent-friendly-404` | Essential | prosody.chat, github.com, gymshark.com, is-optimized.com | `agent.markdown_404` | Implemented site-level response probe; pending candidate live-site verification. |
| `agent-instruction` | Recommended | prosody.chat, gymshark.com, parallel.ai, is-optimized.com | `agent.instructions.when_to_use`, `agent.instructions.quality` | Implemented bounded guidance quality and explicit use-presence checks; candidate live-site evidence remains. |
| `agentic-search-specific` | Recommended | prosody.chat, gymshark.com, parallel.ai | `agent.discoverability.docs` | Search-provider/key dependent; not observed without a configured source. |
| `api-error-model` | Recommended | github.com | `agent.openapi.errors` | Implemented response-schema presence for typed 2xx and error/default responses; no schema quality semantics. |
| `api-schema-analysis` | Recommended | github.com, gymshark.com | `agent.openapi.operations`, `agent.openapi.errors`, `agent.openapi.schema_complexity`, `agent.openapi.patterns` | Implemented static operation and response-shape signals plus schema-node and common API-pattern heuristics; no contract semantic validation. |
| `api-versioning-policy` | Recommended | github.com | `agent.openapi.versioning` | Implemented `info.version` presence; compatibility and deprecation policy remain unverified. |
| `brand-search-accuracy` | Recommended | prosody.chat, is-agentic.com, vercel.com, galileo.ai, is-optimized.com | `agent.discoverability.brand` | Search-provider/key dependent; never infer from page HTML. |
| `cli-tool` | Recommended | galileo.ai, gymshark.com | `agent.developer.cli` | Implemented homepage-link presence heuristic; does not verify installation or package ownership. |
| `content-no-js` | Essential | vercel.com, github.com, galileo.ai, gymshark.com, parallel.ai | `agent.access` | Existing initial-HTML heuristic is partial; no browser execution. |
| `developer-portal` | Recommended | gymshark.com | `agent.developer.portal`, `agent.developer.docs` | Implemented homepage-link heuristic; no linked docs crawl or quality review. |
| `function-calling-compat` | Recommended | github.com, gymshark.com | `agent.openapi.function_calling` | Implemented typed JSON success schema and stable operation-ID heuristic. |
| `json-error-responses` | Essential | gymshark.com | `agent.api.json_errors` | Implemented for errors observed from at most two safe unauthenticated GETs; unknown if the probes yield no error. |
| `json-ld` | Recommended | prosody.chat, github.com, gymshark.com, is-optimized.com | `geo.entity` | Existing entity-presence heuristic is partial; validation/completeness planned. |
| `markdown-negotiation-vary` | Essential | prosody.chat, github.com, gymshark.com, is-optimized.com | `agent.markdown` | Implemented server-side GET negotiation and `/index.md` fallback; candidate live-site evidence remains. |
| `mcp-resource-listing` | Essential | gymshark.com, parallel.ai | `agent.mcp.resources` | Separate MCP client task; protocol calls are not part of this scanner module. |
| `mcp-server` | Recommended | vercel.com, github.com | `agent.mcp.server` | Implemented advertised endpoint/card discovery and read-only protocol negotiation; no authenticated or target-mutating operations. |
| `metadata-completeness` | Recommended | prosody.chat, gymshark.com, is-optimized.com | `seo.social_metadata` | Implemented for sampled homepage metadata; candidate scan evidence remains to be collected. |
| `oauth-support` | Essential | parallel.ai | `agent.oauth.discovery`, `agent.oauth.pkce`, `agent.oauth.scopes` | Implemented metadata-only discovery and S256/scope checks; candidate live-site evidence remains. |
| `onboarding-friction` | Recommended | vercel.com, gymshark.com | `agent.api.onboarding` | Implemented as bounded unauthenticated health/status response and RFC 9728 challenge observations; no registration/token flow. |
| `openapi-spec` | Essential | github.com, gymshark.com | `agent.openapi.spec` | Implemented bounded JSON/YAML OpenAPI 3 discovery and parsing when an API/developer surface is linked; candidate live-site evidence remains. |
| `org-schema-completeness` | Recommended | prosody.chat, github.com, galileo.ai, gymshark.com, parallel.ai, is-optimized.com | `geo.organization`, `geo.entity_links`, `geo.schema_breadth` | Implemented field, sameAs-count and type-breadth checks; truth and entity ownership are not validated. |
| `pricing-info` | Recommended | gymshark.com | `agent.pricing`, `agent.read.pricing` | Weighted, applicability-aware public pricing/Offer observations using sampled pages; linked-only signals are partial and unavailable pages stay unknown. Commercial accuracy is not verified. |
| `public-api` | Recommended | galileo.ai, gymshark.com | `agent.api.reachability` | Implemented for up to two public, parameter-free OpenAPI GET operations; no API credentials or mutations. |
| `public-api-docs` | Recommended | gymshark.com | `agent.developer.docs` | Implemented homepage-link detection for public API documentation. |
| `rate-limit-headers` | Recommended | vercel.com, github.com, galileo.ai, parallel.ai | `agent.api.rate_limits` | Implemented on safe unauthenticated GET responses; standard RateLimit headers pass, legacy X-RateLimit headers partial. |
| `redirect-hygiene` | Essential | gymshark.com | `seo.redirect_hygiene` | Implemented bounded final-URL/hop observations, meta-refresh and short-page script redirect signals; no browser JavaScript is executed. |
| `scoped-permissions` | Essential | vercel.com, gymshark.com | `agent.openapi.scopes`, `agent.oauth.scopes` | Implemented declared OpenAPI OAuth scopes and OAuth `scopes_supported`; no authorization or token exchange. |
| `sitemap` | Recommended | prosody.chat, is-optimized.com | `seo.sitemap_quality`, `seo.sitemap_freshness` | Implemented bounded validity, same-origin and freshness checks; candidate live-site evidence remains. |
| `trust-anchors` | Recommended | prosody.chat, galileo.ai, gymshark.com, is-optimized.com | `geo.trust.about`, `geo.trust.contact`, `geo.trust.privacy`, `geo.trust.terms` | Implemented bounded probes for about/contact/privacy/terms; candidate live-site evidence remains. |

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
level P0 checks are implemented in source; candidate live comparisons and
empirical calibration remain in progress. API/OAuth/MCP source checks are implemented within the documented bounds. Exact-candidate live comparison and deployment acceptance remain separate and must be recorded before claiming completion.

All new findings must keep stable IDs, pillar/stage, weight and applicability,
proportional verdicts, bounded evidence, remediation examples, and any
defensible specification references. Scanned content remains untrusted input.

## Verification still required

- `geo.author` applicability was corrected: an HTML-only `<article>` container no longer activates the editorial attribution check. Fixture coverage includes `og:type=article`, BlogPosting JSON-LD, and a NewsArticle JSON-LD graph. CI and live-site checks remain pending.
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

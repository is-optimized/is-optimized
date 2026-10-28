# MVP methodology and provenance

This document records the independently implemented MVP check registry. It does not claim parity with the hosted Is Agentic service. The source of executable behavior is `internal/scanner/checks.go`; every score identifies methodology `2026-10-v2` and selection policy `home-links-document-order-v1`.

## Reference provenance

Registry metadata queried for `is-agentic@1.0.1` identifies:

- CLI license declaration: ISC.
- Repository: `https://github.com/vercel-labs/is-agentic.git`, directory `cli`.
- npm SHA-1: `ad5abc9ef569415c6f0ee435f1603c8bdac4aebd`.
- npm integrity: `sha512-mVzG/msZFwyj7BofwCN8MwkKhp+E1ItN08QTIjLP3s2xGDx/XL/Iy3SyXyFi3j89eu/IF1LaU4ECQw7XOD6Adw==`.

The npm README describes a CLI that retrieves reports or requests a scan through a hosted API. The repository contents request returned HTTP 404 with the available access; this does not distinguish a private repository from a removed repository. Consequently the scanner implementation and license were not available for inspection. The CLI's license does not establish the license of the hosted scanner.

No upstream source was copied, no npm package is used at runtime, and no hosted Is Agentic scans are invoked. These Go checks are an independent implementation of the observable categories discussed in RFC-001. Exact upstream inventory, scoring comparison, and license qualification remain unresolved before any claim of full upstream parity. Their absence does not turn this registry into a certified or validated ranking predictor.

## Weighted registry

Weights are initial engineering choices, not empirically calibrated predictors of rankings, citations, or successful agent actions. Pass earns full weight, partial earns half, fail earns zero. Not-applicable excludes the check from both denominators. Unknown retains its weight in total applicable weight but is excluded from observed weight. Scores are withheld below 80% coverage or when any foundational check is unknown. A known failure is different from an unobservable page.

Each check runs against every selected content page. The pillar score aggregates the resulting page/check observations, so each sampled page contributes equally for a given check. All unsuccessful fetches, non-200 responses, denied robots access, unsupported content types, truncation, and parser-limit failures produce unknown weighted findings, including foundational unknowns. A 403 or bot wall is never interpreted as a fully observed failing site.

| ID | Weight | Applicability and pass/partial/fail rule | Rationale and limit |
| --- | ---: | --- | --- |
| `seo.access` | 3 | Foundational. Pass for usable HTTP 200 HTML; otherwise unknown. | Establishes whether the sample can be assessed. |
| `seo.index` | 2 | Pass without observed noindex; partial when present. | A noindex directive can be intentional; recommendation asks the owner to review intent. |
| `seo.title` | 2 | Fail when absent; partial when reused on distinct observed sample URLs; pass otherwise. | Descriptive title presence is observable; quality and cross-site uniqueness are not inferred. |
| `seo.description` | 1 | Fail when absent; partial when reused on distinct observed sample URLs; pass otherwise. | Description presence does not guarantee a displayed search snippet. |
| `seo.canonical` | 1 | Pass for one valid absolute public HTTP(S) canonical; partial otherwise. | Does not assert that the declared canonical is the correct business choice or fetch its target. |
| `seo.images` | 1 | Not applicable without images; pass when every image has alt; partial otherwise. | Empty alt may be correct for decorative images. Semantic accuracy is not verified. |
| `geo.access` | 3 | Foundational. Pass for usable HTTP 200 HTML; otherwise unknown. | Establishes observable public content only. |
| `geo.entity` | 2 | Pass for recognized entity types in parseable JSON-LD; partial otherwise. | Recognizes common entity types, not truth, authority, or complete Schema.org validity. |
| `geo.structure` | 2 | Pass for headings and semantic lists/tables/definitions; partial for headings alone; fail without meaningful headings. | Measures extractable organization, not citation probability. |
| `geo.author` | 1 | Applies when article markup is detected. Pass for author metadata plus datetime; partial otherwise. | Current implementation recognizes HTML author metadata and time elements, not every attribution convention. |
| `agent.access` | 3 | Foundational. Pass for at least 20 initial-HTML words; fail below that, unknown if page unavailable. | A coarse app-shell heuristic. Hidden text, visual rendering and actual tasks are not verified. |
| `agent.landmarks` | 1 | Pass with main and nav elements; partial otherwise. | Initial implementation checks native landmarks, not every equivalent ARIA role. |
| `agent.controls` | 2 | Not applicable without controls; pass for names on all controls; partial otherwise. | Observes common label and ARIA mechanisms; does not execute interactions or claim a complete accessibility audit. |
| `agent.links` | 1 | Not applicable without links; pass for names on all links; partial otherwise. | Observes visible/accessible names, not whether every navigation destination works. |

This inventory and fixture results are review evidence for the MVP, not empirical calibration. Additional checks require explicit registry/version changes and tests. Crawler-specific training policies, alleged universal MCP discovery paths, forced API requirements, and fixed answer-length rules carry no score penalty.

## Informational probes

`llms.txt`, `llms-full.txt`, and the first same-origin sitemap declared in the applicable robots policy are bounded informational probes. They do not award bonus points. The sitemap is fetched but not recursively traversed or claimed to have passed XML/schema validation. No GraphQL introspection, MCP tool invocation, credentials, form submission, or transaction occurs.

## Fetch and storage bounds

The scanner fetches the homepage and at most four eligible same-origin links from its initial HTML, in document order, with at most four child goroutines. It fetches robots before content on every new homepage redirect origin. Missing robots (404/410) permits fetching; unavailable, malformed, or denying policies leave content unobserved. Child redirects remain on the final homepage origin. Policies use matching user-agent groups, longest matching path rules, wildcard/end markers, and allow precedence on ties.

Limits are 32 requests including redirects, 8 discovery requests including robots, 3 redirects per fetch, 5 seconds per request, 15 seconds overall, 2 MiB decoded response bodies, 12 MiB aggregate decoded bytes, 50,000 HTML tokens, and a nesting guard of 512. No target URL may exceed 2,048 bytes. Findings retain bounded 300-character text evidence rather than response bodies or HTML trees. With five pages and this registry, serialized report data remains comfortably below the store's 850 KiB ceiling; the store independently enforces that ceiling.

The transport resolves all destination addresses, rejects non-public/mixed public-private answers, and dials an exact validated address while HTTP/TLS retains the requested hostname. It uses no environment proxy. Reserved IPv4, IPv6, mapped addresses, metadata, link-local and transition-prefix cases are covered by focused tests. This is read-only source/test evidence, not a deployed penetration test.

## Verification

`GOMAXPROCS=2 go test -p 1 ./internal/scanner` passes with synthetic transports and no real target scans. Tests cover depth/concurrency, robots before redirected content, unavailable policies, wildcard precedence, address rejection, domain normalization, response limits, request/discovery budgets, URL length, applicability, and score withholding. Integration, Nanos runtime qualification, and production load measurements are separate delivery evidence.

## v2 sampled SEO additions

The v2 registry adds `seo.headings` (weight 1) and `seo.viewport` (weight 1). Heading checks fail when no nonempty H1 exists, are partial for an empty H1 alongside a nonempty one or an upward heading-level skip, and pass otherwise. Multiple meaningful H1s are reported as a count without an automatic penalty. Viewport passes for an observed `width=device-width` declaration and is partial otherwise; this does not test rendered mobile usability. Unobserved pages remain unknown.

Existing title and description checks become partial for equal complete values after case and whitespace normalization across distinct observed final URLs. Redirect aliases count once. This uses the existing metadata weights rather than adding duplicate penalties. Missing values still fail. A single observed page cannot establish full-site uniqueness.

Canonical checks recognize tokenized rel values, flag absent, multiple or malformed absolute HTTP(S) references, and explain a different canonical destination without penalizing it solely for differing from the fetched URL. Targets are not fetched and intentional consolidation is not treated as a defect. HTTP Link-header canonicals are not evaluated.

Zero-weight findings expose sampled-page outcomes, initial-HTML word counts, and HSTS header presence. Requested and final URLs distinguish completed redirects. Unknown fetch outcomes remain distinct from observed HTTP statuses. Word counts exclude head/script/style text but do not establish visible word counts or content quality. HSTS presence is a security observation, not validated transport-policy configuration or an SEO score input. No new requests, crawl depth, or performance measurements are introduced. HTML and PDF show these findings from the same saved evidence.

New fixture tests in `observations_test.go` passed on 2026-10-28 after an explicitly authorized retry passed shared-Mac admission. The complete `go test -race -p 1 ./...` suite and `go vet -p 1 ./...` also passed. These are synthetic/local execution results, not Nanos, provider, or production qualification.

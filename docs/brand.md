# is-optimized minimal interface

This direction supersedes the previous ivory/serif editorial treatment. The owner supplied an is-agentic report screenshot as a hierarchy and simplicity reference: near-black canvas, restrained sans-serif type, simple scores, thin rules, dense evidence rows, and disclosure groups. Third-party branding, scoring formulas, and observed-agent-task claims are not copied.

## Identity and tokens

The original bracket/check mark remains compact. The wordmark is neutral sans-serif, with no oversized sales headline or stock graphics. Embedded SVG assets and all frontend assets ship in the Go binary; there are no new frontend dependencies or external fonts.

| Token | Value | Use |
| --- | --- | --- |
| Background | `#0b0d0c` | Dark default canvas |
| Surface | `#141715` | Inputs/brief and compact utility surfaces |
| Ink | `#eff1ed` | Primary type and action |
| Muted | `#9ba39c` | Secondary text |
| Rule | `#2a302c` | Flat section dividers |
| Accent | `#b7df9c` | Small readiness mark, focus, progress |

System sans-serif is used throughout. Status labels remain textual; muted red/yellow are reserved for failed/partial observations, not additional brand accents. Controls have visible keyboard focus, minimum 44px target height, and no motion under reduced-motion preference. Native `<details>` elements provide keyboard-accessible technical groups. Print uses a light canvas, embedded Go fonts in PDF, thin rules, and compact score columns. Browser printing opens the technical groups and restores their previous state afterward; use the generated PDF for complete evidence when browser print support is limited.

## Visitor sequence

1. **Landing:** compact description and domain form, explicitly curated featured links, and at most six genuine recent public reports. Featured sites are configured in `internal/server/directory.go`; they have no invented score or implied measured endorsement. An empty or unavailable directory is explicit.
2. **Progress:** an indeterminate activity line and factual status text. No invented percentages or promises about completion time. Failed requests restore the form with an understandable message.
3. **Delivery:** a separate optional SMS choice, never an inline full report. Text is prominent; “View report without SMS” is always available. SMS-disabled deployments show the unavailable text action and working report bypass. Optional name and unchecked marketing consent appear only when SMS is enabled. The supplied disclosure is displayed verbatim. No typed phone, OTP, account, or compulsory consent is introduced; the inbound Twilio sender remains the destination authority.
4. **Dedicated report:** the primary route is `/{domain}`, e.g. `/example.com`. It shows the latest saved report, observation timestamp, three separate scores/coverage, prioritized fixes, expandable technical evidence, developer brief, scope, snapshot link, and printable PDF. `/reports/{id}` and `/reports/{id}/pdf` remain exact-version routes. PDF and HTML use the same immutable evidence without rescoring.

Unknown scores say “Insufficient evidence,” never zero. SEO means search engine readiness; GEO means generative search readiness; AEO means AI-agent readiness in this product, not answer-engine optimization. Static checks do not demonstrate completed AI tasks. No name, phone, or marketing permission is needed for report access.

## Preview and acceptance

Task previews: `/Volumes/BuildOffload/artifacts/is-optimized-astra-minimal-20261027/preview/index.html`. The preview toolbar links landing/empty, synthetic recent entries, progress, SMS-disabled consent, simulated SMS-enabled consent/handoff, and dedicated report. All sample activity/scores are labeled in preview-only chrome, never production UI. Preview report/PDF actions do not create a real scan or message. The preview interpreter is not Go template execution.

No browser binary, Go build/test, or PDF generation was run in this continuation because the coordinator's capacity refusal remains terminal. Required visual acceptance: desktop and 320/390px mobile, keyboard navigation, 200% zoom, reduced motion, overflow, all stages, unknown/zero scores, report accordions, and PDF text/links/pagination. Source review is separate from these gates. Deployment belongs exclusively to the coordinator.

# is-optimized brand and interface

A small-business owner should leave with a clearer understanding of their website and a practical next step. The voice is direct, calm, and specific. Explain what was observed before suggesting a fix. Do not promise rankings, AI citations, conversions, scan speed, boot time, or successful agent tasks.

## Identity

The original **observation mark** frames a small check and a point of evidence inside two brackets. The brackets refer to readable website structure; the check represents an observed finding, not a blanket certification. Use the mark as a favicon or compact identifier. The supplied SVG wordmark pairs it with an editorial serif name. Its text remains system Georgia/serif; an outlined, font-independent wordmark is a future production-artwork refinement, not a licensed custom typeface claim.

Assets live in `internal/render/assets/` and are embedded into the Go binary. `mark.svg` is square with a 48-unit viewbox; `wordmark.svg` is horizontal. Preserve the proportions and at least half a mark-width of surrounding space. Do not recolor by score or turn the mark into a customer certification badge. Both assets have an accessible title; HTML also supplies alternative text.

## Tokens

| Token | Value | Use |
| --- | --- | --- |
| Paper | `#faf7ef` | Warm ivory canvas |
| Ink | `#252a27` | Primary text |
| Teal | `#205f59` | Identity, actions, observed evidence |
| Deep teal | `#164740` | Action hover |
| Wash | `#edf1e9` | Quiet panels and report context |
| Line | `#d6d9ce` | Dividers and card edges |
| Muted | `#59625b` | Secondary text |
| White | `#fffefa` | Forms and finding cards |
| Warning | `#80541d` | Partial observations |
| Error | `#9b382d` | Request failures |

`brand.css` is the shared source of truth for browser and print tokens. Use Georgia for editorial headings and system sans for controls, labels, and evidence. PDF embeds Go Regular and Go Bold for portable, selectable text; it carries the same colors, hierarchy, vector mark, status vocabulary, evidence order, and provenance rather than relying on unavailable system fonts.

Layout uses a restrained 1px rule, 4px control radius, generous whitespace, and a 1180px maximum reading layout. No external font service, stock imagery, client framework, or Node production dependency is introduced. Cards stack below 640px. The navigation and form remain usable at narrow widths; keyboard focus has a visible outline. Motion is limited to a working indicator and disabled under reduced-motion preference.

## Product language and states

- **SEO:** search engine readiness. **GEO:** generative search readiness. **AEO:** AI-agent readiness in this product; not answer-engine optimization.
- Show evidence coverage beside each score. A withheld score says **Insufficient evidence**, never 0. A selected page may have failed; do not label all selected pages “successfully scanned.”
- Findings say **Needs attention**, **Room to improve**, **Not observed**, **Observed pass**, **Not applicable**, or **For context**. Text accompanies all status colors.
- Present actionable findings first without changing saved evidence or recomputing scores. HTML and PDF both sort a copy with the same stable order.
- Progress uses honest activity text, not invented percentages. Errors keep the form usable. A new scan clears the optional marketing selection and invalidates any older SMS handoff response in flight.
- The result links to `/reports/{id}` and `/reports/{id}/pdf`, preserving the exact snapshot. The existing domain route remains the latest-report route.
- **Text me the report** remains visible but disabled until SMS is enabled through its separate gates. **View report online** and **Download PDF** need neither name nor marketing permission.
- SMS-enabled form: optional name, unchecked marketing consent, approved disclosure, policy links, composer/manual fallback, and explicit “you send it” instruction. No marketing or calls are implemented.

## Visual acceptance still required

This source refinement has not been visually accepted. Inspect desktop (1440px), mobile (390px and 320px), 200% zoom, keyboard-only navigation, reduced motion, working/error/unknown states, disabled SMS, and simulated enabled-SMS handoff. Inspect generated PDF pages for font embedding, text extraction, working links, and pagination. Use the checklist and receipts in `docs/verification.md`; source and a static preview are not browser or PDF acceptance.

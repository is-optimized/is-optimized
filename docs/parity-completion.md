# Parity completion

Owner direction: complete the remaining scanner parity work, obtain independent
review, merge, qualify the Nanos release, and deploy through the existing IaC.
PR #8 is the merged starting point, not full parity acceptance.

## Accepted scope

- Implement the remaining applicable technical checks in the parity inventory.
- Reuse the scanner's SSRF protections, bounded fetching, evidence model,
  applicability rules, and report computation.
- Complete the public machine-readable interfaces and their documentation.
- Compare the eight requested sites and explain verdict disagreements.
- Verify API, HTML, PDF, developer brief, scan timing, and deployed behavior.
- Resolve the request-budget reproducibility finding from PR #8.

## Explicit exclusions and dependencies

Paid LLM calls, simulated agent tasks, and LLM answerability evaluations are
excluded by the owner. Behavioral evaluation stays `not_run`. Agent traffic
analytics is a separate future project.

Search-provider and PageSpeed measurements require configured credentials;
without them, report not observed with the reason. Their absence must not block
implementation and qualification of the other checks. No fabricated scores or
claims of observed search ranking are permitted.

Twilio campaign work is being handled separately by the owner. Existing SMS
approval and suppression gates remain in force during the parity release.

## Acceptance

Each parity row must map to implemented and verified behavior, justified
inapplicability, or an explicit unsupported dependency. Source, fixture tests,
live-site evidence, Nanos qualification, and production deployment are recorded
separately. Completion requires the live deployed revision to be identified and
its scan/report flow verified.

#!/usr/bin/env python3
"""Compare captured issue-only Is Agentic reports with scanner JSON receipts.

No network calls, third-party packages, provider effects, or score calibration.
Output is deterministic for the supplied files, mapping, and revision label.
"""

import argparse
from collections import Counter, defaultdict
import hashlib
import html
import json
from pathlib import Path
import re
import sys
from urllib.parse import urlsplit, urlunsplit


DOMAINS = (
    "prosody.chat", "is-agentic.com", "vercel.com", "github.com",
    "galileo.ai", "gymshark.com", "parallel.ai", "is-optimized.com",
)
VERDICTS = {"failed": "fail", "failure": "fail", "passed": "pass"}
OBSERVED = {"pass", "partial", "fail"}


def read_json(path):
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path.name}: expected a JSON object")
    return value


def fingerprint(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def mapping_rows(path):
    """Read the declared atomic issue table, not unrelated planning tables."""
    rows = {}
    active = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.startswith("| Upstream issue ID |"):
            active = True
            continue
        if not active:
            continue
        if not line.startswith("|"):
            break
        columns = [item.strip() for item in line.strip().strip("|").split("|")]
        if len(columns) < 5 or columns[0].startswith("---"):
            continue
        upstream = re.findall(r"`([^`]+)`", columns[0])
        targets = re.findall(r"`([^`]+)`", columns[3])
        if len(upstream) != 1 or not targets:
            raise ValueError(f"malformed atomic mapping row: {line}")
        if upstream[0] in rows:
            raise ValueError(f"duplicate upstream mapping: {upstream[0]}")
        rows[upstream[0]] = {"targets": targets, "note": columns[4]}
    if not rows:
        raise ValueError("atomic upstream issue mapping table not found")
    return rows


def safe_url(value):
    """Keep public paths in evidence but omit credentials, query, fragment."""
    try:
        parts = urlsplit(str(value))
        if parts.scheme not in {"http", "https"} or not parts.hostname:
            return "[non-HTTP URL omitted]"
        host = parts.hostname
        if parts.port:
            host += ":" + str(parts.port)
        return urlunsplit((parts.scheme, host, parts.path or "/", "", ""))
    except ValueError:
        return "[invalid URL omitted]"


def clean(value, limit=180):
    value = " ".join(str(value).split())
    value = re.sub(r"https?://[^\s<>\"']+", lambda match: safe_url(match.group(0)), value)
    value = re.sub(r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+", "Bearer [redacted]", value)
    return value[:limit] + ("…" if len(value) > limit else "")


def md(value):
    value = html.escape(str(value), quote=False)
    for char in ("\\", "|", "`", "[", "]", "*", "_"):
        value = value.replace(char, "\\" + char)
    return value


def normalize_url(value):
    # URL paths are case-sensitive; safe_url normalizes scheme/host only.
    return safe_url(value).rstrip("/")


def method_rationale(issue_id):
    return {
        "content-no-js": "Upstream combines initial-text readability with content ratio and heading structure. The declared agent.access mapping measures initial-HTML readability; geo.content_ratio and seo.headings are separate candidate checks to review when expanding this composite mapping. Neither result establishes browser behavior.",
        "metadata-completeness": "Upstream describes four homepage metadata signals; candidate social metadata covers eight signals and attempts image response validation, so proportional scores need not match.",
        "trust-anchors": "Upstream groups About, Contact and Privacy; candidate mapping also includes Terms and reports pages separately. Grouped severity and extra requirements differ.",
        "brand-search-accuracy": "Search-provider evidence is required. Unavailable candidate search evidence produces unknown, not a brand-ranking failure or pass.",
        "agentic-search-specific": "Web-search ranking requires search-provider evidence. Linked-doc discovery is not equivalent to off-site search ranking.",
        "mcp-resource-listing": "Candidate MCP applicability follows advertised interfaces and permitted protocol responses; no interface, denied access and an observed empty list are distinct outcomes.",
        "mcp-server": "An advertised endpoint, completed initialization and registry listing are distinct evidence levels; discovery alone does not establish a working server.",
        "openapi-spec": "Candidate API applicability follows advertised API/developer surfaces and bounded probes. Excluding an informational site differs from treating absent OpenAPI as a universal failure.",
        "oauth-support": "Candidate auth applicability follows advertised protected/API surfaces; public documentation need not require OAuth. Metadata does not exercise login.",
        "public-api": "Candidate probes only advertised safe unauthenticated GET operations. No safe operation, unavailable responses and observed API errors are distinct states.",
        "onboarding-friction": "Candidate evidence is limited to safe unauthenticated GET responses, not registration, account creation or credential issuance.",
        "rate-limit-headers": "Observations depend on which safe endpoint responded. Headers on a homepage, API response and blocked request are not interchangeable evidence.",
        "json-ld": "Mapped entity presence is narrower than JSON-LD syntax, context and type-specific completeness; presence does not certify identity or factual truth.",
        "org-schema-completeness": "The mapping separates identity fields, entity links and schema breadth. Those subchecks and applicable entity types differ from the grouped upstream Organization rule.",
        "agent-friendly-404": "Captures probe different random paths at different times. Correct HTTP status alone does not establish a Markdown error representation.",
        "markdown-negotiation-vary": "Homepage negotiation and separate Markdown files are different surfaces; candidate partial credit can retain a fallback when homepage negotiation fails.",
        "redirect-hygiene": "Candidate evidence uses the sampled homepage when available. Fetch redirects, refresh stubs and broader upstream sampling can yield different composite verdicts.",
    }.get(issue_id, "Inspect the bounded response evidence and declared mapping scope; captures differ in time and do not prove identical check definitions.")


def candidate_groups(report):
    groups = defaultdict(list)
    for finding in report.get("findings", []):
        if isinstance(finding, dict) and isinstance(finding.get("id"), str):
            groups[finding["id"]].append(finding)
    pages = report.get("pages") or []
    homepage = normalize_url(pages[0].get("url", "")) if pages else ""
    # Upstream issues describe the scanned homepage. Use homepage evidence when
    # a mapped per-page ID has it; keep all records for site-level target probes.
    for key, values in list(groups.items()):
        home_values = [f for f in values if homepage and normalize_url(f.get("url", "")) == homepage]
        groups[key] = sorted(home_values or values, key=lambda f: (
            str(f.get("url", "")), str(f.get("status", "")), str(f.get("evidence", ""))))
    return groups


def compare_issue(issue, targets, groups):
    statuses = []
    rendered = []
    for target in targets:
        findings = groups.get(target, [])
        if not findings:
            statuses.append("missing")
            rendered.append(md(target + ": missing — no candidate finding emitted"))
            continue
        counts = Counter(str(f.get("status", "unknown")) for f in findings)
        statuses.extend(counts)
        detail = ", ".join(f"{status}×{counts[status]}" for status in sorted(counts))
        examples = []
        for finding in findings[:2]:
            examples.append(clean(finding.get("evidence", ""), 140) + " [" + safe_url(finding.get("url", "")) + "]")
        if len(findings) > 2:
            examples.append(f"{len(findings)-2} further records in report JSON")
        rendered.append(md(target + ": " + detail + " — " + "; ".join(examples)))
    observed = set(statuses) & OBSERVED
    upstream = VERDICTS.get(str(issue.get("result", "")), str(issue.get("result", "")))
    if observed == {upstream}:
        outcome = "verdict agreement"
        reason = "Mapped observed verdicts match the reported upstream issue; methods may still differ."
    elif observed == {"pass"}:
        outcome = "disagreement: candidate passes"
        reason = "Candidate observed success where the earlier upstream capture reported a problem; inspect timing and mapping limits."
    elif observed:
        outcome = "severity/scope difference"
        reason = "Both captures expose a problem, but candidate subchecks or sampled severity differ; not an exact verdict agreement."
    elif "unknown" in statuses or "missing" in statuses:
        outcome = "not observed"
        reason = "Candidate lacks a verified verdict for at least one mapped check; unknown/missing is not a failure or pass."
    elif "not_applicable" in statuses:
        outcome = "applicability difference"
        reason = "Candidate excludes the mapped surface from scoring; upstream reported it as an issue. Review the applicability rule."
    else:
        outcome = "observation only"
        reason = "Mapping resolves to discovery/information only; no equivalent scored verdict established."
    excluded = set(statuses) - OBSERVED
    if observed and excluded:
        reason += " Additional mapped results: " + ", ".join(sorted(excluded)) + "."
    if outcome != "verdict agreement":
        reason += " " + method_rationale(issue["id"])
    return outcome, reason, "<br>".join(rendered)


def score(report, pillar):
    for value in report.get("pillars", []):
        if value.get("id") == pillar:
            result = value.get("score")
            return "withheld" if result is None else str(result)
    return "not captured"


def render(args):
    mapping = mapping_rows(args.mapping)
    captures = []
    issue_ids = set()
    for domain in DOMAINS:
        baseline_path = args.baseline_dir / (domain + ".json")
        report_path = args.candidate_dir / (domain + ".report.json")
        scores_path = args.candidate_dir / (domain + ".scores.json")
        baseline, report, scores = read_json(baseline_path), read_json(report_path), read_json(scores_path)
        baseline_domain = baseline.get("display_target") or urlsplit(str(baseline.get("target", ""))).hostname
        if str(baseline_domain).lower().rstrip(".") != domain:
            raise ValueError(f"upstream domain identity mismatch: {domain}")
        if report.get("domain") != domain or scores.get("domain") != domain:
            raise ValueError(f"candidate domain identity mismatch: {domain}")
        if report.get("methodology_version") != scores.get("methodology_version") or report.get("pillars") != scores.get("pillars") or report.get("agent_readiness") != scores.get("agent_readiness"):
            raise ValueError(f"candidate report/score receipt mismatch: {domain}")
        issues = baseline.get("issues", [])
        if not isinstance(issues, list):
            raise ValueError(f"{domain}: baseline issues must be a list")
        for issue in issues:
            if not isinstance(issue, dict) or not isinstance(issue.get("id"), str):
                raise ValueError(f"{domain}: malformed upstream issue")
            if VERDICTS.get(str(issue.get("result")), str(issue.get("result"))) not in {"fail", "partial"}:
                raise ValueError(f"{domain}: upstream issue is not failed/partial; review expanded schema")
            issue_ids.add(issue["id"])
        captures.append((domain, baseline, report, scores, baseline_path, report_path))
    missing = issue_ids - set(mapping)
    if missing:
        raise ValueError("unmapped returned upstream issue IDs: " + ", ".join(sorted(missing)))

    lines = ["# Captured agent-readiness comparison", "",
        "Candidate source receipt: " + md(args.candidate_revision or "not supplied") + ".",
        f"The eight upstream captures return {len(issue_ids)} distinct failed/partial issue IDs; the mapping contains {len(mapping)} atomic rows. This compares returned issues only. Upstream passing-check definitions were not returned, so absent issues are not inferred passes and this is not proof of full check parity.", "",
        "All inputs are existing local JSON receipts. This report makes no network calls. Differences can reflect capture time, site changes, scope, applicability, provider access, or method. Equal scores or verdicts do not establish equivalent behavior.", "",
        "## Scores and capture times", "",
        "Upstream and candidate scores use different methodologies; these columns are a cross-method comparison, not a before/after improvement claim. Candidate SEO, GEO, AEO and Agent readiness are separate computed scores.", "",
        "| Domain | Upstream capture | Upstream score | Candidate capture | SEO | GEO | AEO | Agent | Pages | Elapsed ms |", "| --- | --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |"]
    for domain, baseline, report, scores, _, _ in captures:
        values = [domain, baseline.get("scanned_at", "not captured"), baseline.get("score", "not captured"), report.get("started_at", "not captured"), score(report,"seo"), score(report,"geo"), score(report,"agent"), score(report,"agent_readiness"), len(report.get("pages",[])), scores.get("elapsed_ms","not captured")]
        lines.append("| " + " | ".join(md(v) for v in values) + " |")
    lines += ["", "## Same-product before/after", ""]
    if args.previous_dir:
        lines += ["Only supplied previous is-optimized JSON/timing receipts are used; missing values are not reconstructed from expectations.", "", "| Domain | Previous capture | Previous SEO / GEO / AEO / Agent | Candidate SEO / GEO / AEO / Agent | Previous ms | Candidate ms |", "| --- | --- | --- | --- | ---: | ---: |"]
        for domain, _, report, scores, _, _ in captures:
            old_report_path = args.previous_dir / (domain+".report.json")
            old_scores_path = args.previous_dir / (domain+".scores.json")
            old = read_json(old_report_path) if old_report_path.exists() else {}
            old_scores = read_json(old_scores_path) if old_scores_path.exists() else {}
            values = [domain, old.get("started_at","not captured"), " / ".join(score(old,p) for p in ("seo","geo","agent","agent_readiness")), " / ".join(score(report,p) for p in ("seo","geo","agent","agent_readiness")), old_scores.get("elapsed_ms","not captured"), scores.get("elapsed_ms","not captured")]
            lines.append("| "+" | ".join(md(v) for v in values)+" |")
    else:
        lines.append("No previous is-optimized JSON/timing directory was supplied. Same-product before/after score and latency deltas are therefore not established. The earlier prompt's timing/score expectations are not measurements of this run.")
    totals = Counter()
    for domain, baseline, report, scores, _, _ in captures:
        groups = candidate_groups(report)
        lines += ["", "## " + domain, "", "Upstream capture: " + md(baseline.get("scanned_at","not captured")) + "; candidate interval: " + md(report.get("started_at","not captured")) + " to " + md(report.get("completed_at","not captured")) + ". Candidate selection policy: " + md(report.get("selection_policy","not captured")) + "; methodology: " + md(report.get("methodology_version","not captured")) + ".", "", "| Returned upstream issue / verdict | Candidate mapped observations | Comparison | Explanation / mapping boundary |", "| --- | --- | --- | --- |"]
        issues = sorted(baseline.get("issues",[]),key=lambda i:i["id"])
        if not issues:
            lines.append("| No failed/partial issues returned | No passing checks inferred | no returned issues | An empty upstream issue list does not disclose the checks that passed. |")
        for issue in issues:
            row = mapping[issue["id"]]
            outcome, reason, observed = compare_issue(issue,row["targets"],groups)
            totals[outcome] += 1
            upstream = issue["id"] + " / " + str(issue.get("result","not captured")) + " — " + clean(issue.get("details",""),160)
            lines.append("| " + md(upstream) + " | " + observed + " | " + md(outcome) + " | " + md(reason + " Mapping: " + clean(row["note"],200)) + " |")
    lines += ["", "## Comparison counts", "", "Counts below concern returned issue rows, not total check coverage or independent correctness.", "", "| Classification | Issue rows |", "| --- | ---: |"]
    lines += ["| "+md(key)+" | "+str(totals[key])+" |" for key in sorted(totals)]
    lines += ["", "Homepage evidence is selected for a mapped per-page ID when present; otherwise all findings for that ID are retained. Up to two bounded evidence examples are displayed per ID; complete findings remain in the report JSON. A grouped issue can map to several narrower checks, so mixed results are reported as scope/severity differences. Unknown, not applicable, missing, and information are preserved as distinct states.", "", "## Input fingerprints", "", "| Input | SHA-256 |", "| --- | --- |", "| atomic mapping | "+fingerprint(args.mapping)+" |"]
    lines.append("| comparator source | "+fingerprint(Path(__file__))+" |")
    for domain, _, _, _, baseline_path, report_path in captures:
        lines.append("| "+domain+" upstream JSON | "+fingerprint(baseline_path)+" |")
        lines.append("| "+domain+" candidate report JSON | "+fingerprint(report_path)+" |")
        lines.append("| "+domain+" candidate scores/timing JSON | "+fingerprint(args.candidate_dir / (domain+".scores.json"))+" |")
    return "\n".join(lines)+"\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-dir", type=Path, required=True)
    parser.add_argument("--candidate-dir", type=Path, required=True)
    parser.add_argument("--mapping", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--candidate-revision", default="")
    parser.add_argument("--previous-dir", type=Path)
    args = parser.parse_args()
    try:
        body = render(args)
        # Preserve previous comparison receipts. Write a new explicit output path
        # for a later candidate rather than replacing an uncertain earlier run.
        with args.output.open("x",encoding="utf-8") as destination:
            destination.write(body)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("comparison failed: "+str(error),file=sys.stderr)
        return 1
    print(str(args.output))
    return 0


if __name__ == "__main__":
    sys.exit(main())

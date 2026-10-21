# Code analysis: offline reference pack

Read [REPORT_FORMAT.md](REPORT_FORMAT.md) before writing the report. It supplies the
complete report skeleton, finding fields, evidence rules and verification format.
The archive procedure remains in [the skill](../SKILL.md).

These local files are sufficient for an offline review. Resolve their paths relative
to this skill's directory, not the repository being reviewed. No website, hosted
model, scanner, package install or benchmark framework is required. Use available
local source and tools; record unavailable checks rather than downloading tools.
An offline dependency review cannot establish the latest vulnerability advisory status.

## Source references (optional reading)

This pack is project-authored guidance informed by the sources below, not a mirror
of their pages, an OWASP certification or a required online checklist.

| Source | Guidance included locally |
| --- | --- |
| [OWASP WSTG reporting](https://owasp.org/www-project-web-security-testing-guide/v42/5-Reporting) | Point-in-time scope, executive summary, limitations, reproducible findings, impact and remediation |
| [Google Benchmark user guide](https://google.github.io/benchmark/user_guide.html) | Workload/environment context, repeated measurements, units, variability and baseline comparisons |

Cite the bundled reference actually read. External URLs may be retained as background
with `Not accessed (offline)`; do not fabricate access dates or claim to have consulted
the current site. Optional online research must be permitted by the caller/environment
and must not disclose private code. A failed optional lookup does not block the report.

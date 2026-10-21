# Documentation: offline templates

This pack is sufficient to maintain documentation without internet or another skill.
Resolve links relative to the installed skill, not the project being documented.
Read this file, [DIAGRAMS.md](DIAGRAMS.md) and [COMMIT_FORMAT.md](COMMIT_FORMAT.md)
before writing. Reuse existing project pages/layouts instead of forcing these filenames.
The examples are templates, not evidence that the target project has these resources.

## Root README.md template

Replace placeholders with source-backed facts. Omit inapplicable optional sections;
never invent commands, support promises or licenses. Keep useful existing content.
For a missing README, start with:

````markdown
# <Project name>

<What the project does, intended users, and current scope.>

## Requirements
<Supported local tools/runtime versions from configuration; optional dependencies.>

## Getting Started
<Smallest verified setup and run procedure. Label anything not executed.>

```sh
<actual project command; omit this block if no verified command is known>
```

## Usage
<Minimal source-backed example and expected behavior.>

## Configuration
| Setting | Default | Purpose |
| --- | --- | --- |
| <actual key> | <observed default or Required> | <behavior; no credentials> |

## Architecture and Documentation
- [Documentation index](docs/README.md)
- [Architecture](docs/architecture.md)
- [Resources and relationships](docs/resources.md)

## Development and Verification
<Actual local build/test/docs commands; requirements and offline limitations.>

## Limitations
<Important supported boundaries and known unknowns.>
````

Keep a short overview diagram in the README only when useful; link detailed diagrams
rather than duplicating the same graph in several pages. Add license/contribution/help
links only when those files or policies actually exist. Adapt links to the real tree.

## docs/README.md template

```markdown
# Documentation

<Audience, scope and how this documentation relates to the root README.>

## Guides
- [Architecture](architecture.md): boundaries, responsibilities and main flows.
- [Resources](resources.md): definitions, ownership and dependencies.
<Add configuration, interfaces and operations pages when warranted by the code.>

## Maintaining These Docs
<Source paths to review when behavior changes, local validation commands and diagram
format. Record validation limitations rather than claiming all examples were run.>

## References
<Bundled guidance actually consulted, plus relevant external URLs marked as
background / Not accessed (offline), or actual access dates for permitted lookups.>
```

Architecture and resource page templates are in [DIAGRAMS.md](DIAGRAMS.md).
Longer projects may need operations/configuration/API pages: document actual entry
points, inputs/defaults, output/errors, permissions, lifecycle and safe troubleshooting,
with source links. Do not create empty pages to satisfy an arbitrary page count.

## Offline validation checklist

1. Compare commands, configuration/defaults and diagrams to the actual code and tests.
   Check every node/edge and table dependency. Update existing content surgically.
2. Resolve relative document/source links from each output page, including filename
   case and anchors. Check code fences and readable text equivalents for diagrams.
3. Run available project documentation checks and safe local examples. In offline
   mode, do not run commands that fetch packages or call remote services. Missing
   dependencies mean `Not run` with the reason; no automatic download is required.
4. Parse/render diagrams only with an already installed compatible renderer. Without
   one, keep editable diagram source and a text/table equivalent, review syntax and
   label rendering `Not verified (renderer unavailable)`. This optional limitation
   alone does not block a commit unless project policy requires rendered validation.
5. Review the diff for invented claims, secrets, irrelevant churn and unrelated edits.
   Required check failures block the commit. Use the skill's Git safety protocol and
   [COMMIT_FORMAT.md](COMMIT_FORMAT.md); no empty commit on a no-change run.

## Source references (optional reading)

These are project-authored templates informed by the following sources, not downloaded
copies. Their useful principles are included here; fetching them is not a prerequisite.

| Source | Guidance included locally |
| --- | --- |
| [GitHub: About READMEs](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-readmes) | Purpose, requirements, getting started, usage and navigation |
| [GitHub Copilot: Document code](https://docs.github.com/en/copilot/tutorials/copilot-chat-cookbook/document-code) | Ground generated explanations in source and synchronize with code changes |
| [GitHub Copilot: Creating diagrams](https://docs.github.com/en/copilot/tutorials/copilot-chat-cookbook/communicate-effectively/creating-diagrams) | Editable Mermaid output; AI output needs verification against actual code |
| [GitHub: Creating diagrams](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-diagrams) | Fenced Mermaid source in Markdown |
| [C4 model: Diagrams](https://c4model.com/diagrams) | Context/container views, only useful detail levels, explicit relationships |

Cite bundled guidance as local guidance. Retain external URLs as background with
`Not accessed (offline)` when appropriate. Never invent a web access date. Online
research is optional and subject to caller/environment permission; do not send
private source to search services or hosted diagram renderers.

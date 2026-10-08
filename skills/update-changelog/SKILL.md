---
name: update-changelog
description: Create or update a project changelog from committed changes unique to the current Git branch, refreshing the current release date from VERSION. Use ONLY when the user explicitly invokes /skill:update-changelog; never activate from task relevance or natural-language requests.
disable-model-invocation: true
---

# Update Changelog

Activation gate: Execute this skill ONLY with the host's explicit user-invocation provenance for `/skill:update-changelog`, or expressly authorized delegated context carrying that provenance and assignment. The literal command need not survive in the request. Loading `skill://update-changelog`, mentions, configuration, automatic loading, recommendations, task relevance, and natural-language requests alone do not authorize execution. This gate does not prohibit ordinary changelog updates required by the requested code change.

Create or update `CHANGELOG.md` in the project root, or the user-specified path such as `./docs/CHANGELOG.md`.

## Workflow

1. Resolve the Git repository root with `git rev-parse --show-toplevel`. Resolve relative output paths from that root.
2. Determine the current branch and a distinct base branch. Prefer an explicitly requested base; otherwise use the repository's default branch (`origin/HEAD`, then local `main` or `master`). Do not use the current branch's tracking branch as the base automatically. If no safe base exists, ask for one.
3. Inspect every committed change unique to the current branch with the base-to-`HEAD` range. Use the merge base so commits already reachable from the base are excluded:

   ```sh
   base=$(git merge-base HEAD <base-branch>)
   git log --reverse "$base..HEAD"
   git diff --stat "$base...HEAD"
   git diff "$base...HEAD"
   ```

   Ignore staged and unstaged changes. Do not omit merge commits from the review.
4. Read the existing changelog before editing. Preserve its history, ordering, wording style, and headings. If the target does not exist, read [references/changelog-format.md](references/changelog-format.md), then create `# Changelog`, a short project-name introduction, and the release section.
5. When the repository-root `VERSION` file exists and is non-empty, treat its trimmed contents as the current version. If the changelog contains `## [<version>] - YYYY-MM-DD` for that version, update only its date to the current date when it is outdated. Do this even when the commit range is empty, but do not create a release entry solely for this date refresh.
6. If the range is empty, do not add change summaries; report that there are no unique committed changes.
7. Add the changes to an existing `## [Unreleased]` section. Otherwise insert a new section near the top using `## [<version>] - YYYY-MM-DD`, where `<version>` is the trimmed contents of the repository-root `VERSION` file when it exists and is non-empty; otherwise use `Unreleased`. Use a user-provided version/date when supplied.
8. Group concise, evidence-based summaries under `### Features`, `### Bugfixes`, and `### Other`. Add `### Breaking Changes` only when applicable. Omit empty sections and placeholder bullets. Summarize the resulting behavior from the diff; do not invent details or copy unrelated commit messages.

Do not commit the changelog or modify files outside the requested output.

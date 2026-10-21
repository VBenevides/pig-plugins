# Required STE writing rules for skill output

Apply the `ste-writing` rules to all new or revised prose from this skill.
This includes reports, TODO plans and evidence notes, changelogs, release notes,
documentation, commit messages, diagram labels, and final status reports.

These local rules are sufficient without internet access or another installed skill.
They summarize this project's `ste-writing` skill, not the complete ASD-STE100 standard.
Do not claim full standard compliance from these rules alone.

## Modes

- Use **strict** mode for instructions, procedures, safety text, and error messages.
- Use **STE-flavored** mode for descriptive reports, plans, changelogs, and general documents.
- Use a wider technical vocabulary in STE-flavored mode when accuracy requires it.

## Writing rules

1. Use one name for one thing. Give each word one meaning.
2. Use short, common words and American spelling. Avoid marketing claims.
   Use `start`, `use`, and `help`, not `commence`, `utilize`, or `facilitate`.
3. Use active voice and a verb for each action. Avoid unnecessary auxiliary verbs.
   Prefer simple verb forms instead of unnecessary `-ing` forms.
4. Put one instruction in each sentence. Keep instructions at 20 words or fewer.
   Keep descriptive sentences at 25 words or fewer. Do not use contractions.
5. Use articles such as `a`, `an`, and `the`. Do not use semicolons in authored prose.
6. Keep one topic in each paragraph. Limit each paragraph to six sentences.
7. Use numbered lists for procedures. Use imperative verbs for steps.
   Put a condition before its command.
8. Include only requested content and required output sections. Do not add unrelated introductions or closing text.

## Preserve content and formats

Do not change code, identifiers, commands, paths, URLs, hashes, dates, or required machine-readable fields for style.
Preserve exact technical terms, protocol values, and prescribed headings when the output format requires them.
Preserve quotations, raw tool output, and existing test or commit evidence verbatim.
Do not rewrite historical entries or archived reports solely to apply these rules.
Apply STE to surrounding explanations and new summaries instead.

Keep required report sections, tables, checklist fields, and commit-message syntax.
For example, retain `BREAKING CHANGE:` and `docs(component): ...` when required.
A writing rule must not remove a required summary or change the meaning of evidence.
Do not fabricate findings, measurements, test results, access dates, or commit hashes.

## Required final review

Before you save, publish, commit, or return authored prose:

1. Split long sentences. Keep each instruction to one action.
2. Remove contractions, semicolons, and unnecessary complex words from authored prose.
3. Use active voice when the actor is known. Check consistent names and American spelling.
4. Check that technical facts, literal content, and required formats remain unchanged.
5. Record any writing-check limitation honestly. Do not claim that an automatic checker ran unless it did.

No external linter or website is required for this review.
The optional source standard is [ASD-STE100](https://asd-ste100.org).
Do not copy the complete copyrighted standard.

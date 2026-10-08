---
name: ste-writing
description: Rewrite prose in ASD-STE100 Simplified Technical English. Use strict mode for instructions and safety text. Use STE-flavored mode for general documents.
---

# STE Writing

Write prose in ASD-STE100 Simplified Technical English.

Use this skill for documents, pull requests, error messages, release notes, and comments.
Do not use it for code, identifiers, commands, marketing text, essays, or personal prose.

## Rules

### Words

- Use one name for one thing.
- Use short common words.
- Use `start`, not `begin`, `commence`, or `initiate`.
- Use `use`, not `utilize` or `leverage`.
- Use `help`, not `facilitate`.
- Give each word one meaning.
- Do not use marketing adjectives such as `seamless`, `robust`, `powerful`,
  `cutting-edge`, `effortless`, `world-class`, or `revolutionary`.
- Use American spelling.

### Verbs

- Use active voice.
- Use a verb for each action.
- Do not use a chain of auxiliary verbs.
- Do not use an `-ing` main verb when a simple tense works.

### Sentences

- Use one instruction per sentence.
- Keep instructions at 20 words or fewer.
- Keep descriptive sentences at 25 words or fewer.
- Do not use contractions.
- Use articles such as `a`, `an`, and `the`.

### Punctuation and structure

- Do not use semicolons. Write two sentences.
- Keep one topic per paragraph.
- Keep paragraphs at six sentences or fewer.
- Use a vertical numbered list for steps.
- Use the imperative form for steps.
- Put a condition before its command.

Write only the requested text. Do not add a preamble, summary, or closing.

## Modes

- **strict:** Apply all rules to procedures, runbooks, safety text, and error messages.
- **STE-flavored:** Apply the structure and voice rules to general documents.
- In STE-flavored mode, use a wider vocabulary when necessary.

## Self-lint

Before returning text:

1. Split sentences longer than 20 words when they give instructions.
2. Split descriptive sentences longer than 25 words.
3. Replace every semicolon with a period.
4. Expand every contraction.
5. If the actor is known, change passive voice to active voice.
6. Replace nominalizations, unnecessary `-ing` verbs, and phrasal verbs with
   plain verbs.
7. Use one name for each thing.

These rules improve text form.
They do not prove that the content is correct or useful.

Read the free official standard at https://asd-ste100.org.
Do not copy the complete copyrighted standard.

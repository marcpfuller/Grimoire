---
name: implementation-workflow
description: Orchestrate a four-gate workflow for repository changes by gathering focused context, settling the plan, implementing with applicable skills and TDD, then verifying and reporting. Use for implementation work; do not use for pure questions, read-only reviews, or prose-only documentation unless they lead to edits.
---

# Implementation Workflow

Complete every gate in order. A gate may take a justified fast path, but it may not
be silently skipped. Keep a compact gate ledger in the active plan rather than
repeating full skill instructions in chat updates.

## Select Language Guidance

Inspect the repository manifests and the files in scope, then read only the matching
profile:

- Go: [references/go.md](references/go.md)
- Python or Django: [references/python-django.md](references/python-django.md)
- A materially cross-language change: read each affected profile.

For another language, apply the shared gates and follow the repository's established
tooling and instructions. Do not substitute commands from an unrelated profile.

## Gate 0: Tool Readiness

Use the selected profile and repository instructions to check required tools once
per session or when the task needs a tool not yet verified. Do not install, upgrade,
or replace tools silently.

Record the active repository and workspace before selecting a domain-context skill.
Use a context skill only when its documented repository boundary matches that
identity; a skill for the repository currently being changed is not cross-repository
context.

## Gate 1: Focused Source Context

When `$issue-diagnosis-workflow` provides a diagnosis handoff in the current turn,
reuse its issue context and causal evidence. Do not repeat issue lookup or root-cause
diagnosis. Continue only with the local inspection needed to implement safely.

Unless an active handoff already satisfies the issue read, invoke
`$github-issue-context` before inspecting source when a task identifies a GitHub
issue number or URL.

Inspect the relevant implementations, complete type definitions, callers, tests,
configuration, and error paths before editing. Before proposing a helper, search
for APIs that already provide the same data or mechanics and inspect their callers.

Prefer semantic navigation appropriate to the language when it is available. Use
text search for exact literals, generated files, unsupported artifacts, or as a
fallback. Verify conclusions against source rather than treating a search or graph
match as runtime proof.

Select the smallest dependency-context path that answers the task:

1. Use a repository or domain context skill only when its documented trigger matches
   both the active repository and a real cross-repository boundary. Do not use it to
   inspect the repository currently being changed.
2. If focused local inspection establishes the behavior, record dependency analysis
   as not applicable with one sentence.
3. Invoke `$graphify` only when the user requests it or focused semantic and text
   inspection cannot establish a material cross-file relationship.
4. Invoke `$opensrc` when behavior depends on undocumented third-party internals;
   inspect the exact project-selected version.
5. Invoke `$graph-dep` when the task needs a relationship graph across dependency
   internals. It owns the combined opensrc and Graphify workflow.

Summarize only the files, symbols, dependency versions, and confirmed behaviors that
affect the plan.

```text
Context: <relevant local files and symbols>
Dependencies: <evidence gathered, or N/A with reason>
Open assumptions: <none, or concise list>
```

## Gate 2: Settle the Plan

Form a compact plan covering only relevant scope, behavior, security, tests,
migration, rollout, and rollback.

- Treat a clear request or explicit approval as settling decisions it answers.
- Use repository evidence to settle technical details that do not require a product
  choice.
- A new helper must name the closest existing API and explain why reuse or adaptation
  is insufficient. A narrower result or convenient shape alone is not enough.
- If no material decision remains, mark the plan settled and continue.
- If a material decision remains, invoke `$grill-me`, ask one question at a time,
  include a recommendation, and do not edit until the answer settles the branch.

```text
Plan: <files, behavior, and tests in a few lines>
Decision: settled | waiting on <one material question>
```

## Gate 3: Implement

For executable behavior changes:

1. Invoke `$testing-standards` and add or update a focused failing test.
2. Run it and confirm it fails for the intended reason.
3. Make the smallest correct implementation change.
4. Run focused tests, then refactor without changing behavior.

Load other skills only when their trigger applies:

- `$security-standards` for a changed trust boundary or security-sensitive path.
- `$typed-state-first` for loosely typed maps, context values, metadata, serialized
  objects, or parameter bags that control behavior. For untrusted external data,
  validate the runtime shape before indexing or casting it.
- `$code-structure` for proven cross-caller duplication or a necessary module or
  package boundary.

Do not load skills merely to prove they are unnecessary. Instruction,
configuration, and generated-artifact-only changes do not need a contrived failing
unit test; validate their actual consumer behavior instead.

## Gate 4: Verify and Report

Run focused checks while iterating and the full applicable suite once at handoff.
Use the selected language profile and repository instructions; do not claim a check
passed when a missing prerequisite prevented it from running.

Report:

- Gate 1 evidence and justified N/A items
- the settled plan
- security considerations
- tests or direct validations added or changed
- commands and outcomes
- remaining risks or coverage gaps

Keep successful logs concise and store verbose diagnostic output in a disposable,
gitignored location when necessary.

## Token Discipline

- Reuse context gathered in the current turn.
- Prefer targeted searches and file sections over whole-file dumps.
- Query an existing graph before rebuilding it.
- Load only the skill and profile required by the current gate.
- Run non-verbose focused tests during iteration and one full suite at handoff.
- Keep user updates to decisions, evidence, and outcomes.

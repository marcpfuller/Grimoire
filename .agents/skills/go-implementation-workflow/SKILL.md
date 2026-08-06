---
name: go-implementation-workflow
description: "Orchestrate the mandatory four-gate workflow for Grimoire changes to Go code, tests, configuration, build behavior, or generated behavior: gather focused source context, settle the plan with the user, implement with applicable Go skills and TDD, then verify and report. Use for implementation work; do not use for pure questions, read-only reviews, or prose-only documentation unless they lead to edits."
---

# Go Implementation Workflow

Complete every gate in order. A gate may take a justified fast path, but it may not
be silently skipped. Keep a compact gate ledger in the active plan rather than
repeating full skill instructions in chat updates.

## Gate 0: Tool Readiness

Check required tools once per session or when the task needs a tool not yet verified:

```bash
go version
golangci-lint --version
mockery --version
grep '^module ' go.mod
```

Do not install, upgrade, or replace tools silently. This repository currently uses
Mockery v2 through `go tool mockery`.

## Gate 1: Focused Source Context

Always inspect the relevant local implementations, complete type definitions,
interfaces, callers, tests, configuration, and error paths before editing.

Then select the smallest dependency-context path that answers the task:

1. If the task affects the Grimoire-Ghostwriter contract, invoke
   `$ghostwriter-context`. It guarantees a revision-pinned Graphify knowledge graph,
   queries it with a bounded budget, and verifies important results against focused
   Ghostwriter source. Treat its output as the related-repository portion of this
   gate; do not repeat its source or graph work.
2. If focused local search establishes the behavior, record dependency analysis as
   not applicable with one sentence.
3. If an existing `graphify-out/graph.json` can answer a broad cross-file question,
   invoke `$graphify` in query mode rather than rebuilding or rereading the corpus.
4. If behavior depends on undocumented third-party internals, invoke `$opensrc` for
   the exact project-selected version.
5. If the task needs a relationship graph across dependency internals, invoke
   `$graph-dep`; it owns the coordinated `$opensrc` plus `$graphify` workflow, so do
   not separately repeat those steps.
6. Build a new local Graphify graph only when cross-package relationships materially
   affect the implementation and focused source search is insufficient.

Summarize only the files, symbols, dependency versions, and confirmed behaviors that
affect the plan. Do not paste full source files or dependency trees into the chat.

Gate output:

```text
Context: <relevant local files and symbols>
Dependencies: <evidence gathered, or N/A with reason>
Open assumptions: <none, or concise list>
```

## Gate 2: Settle the Plan

Form a compact plan covering only relevant scope, behavior, security, tests,
migration, rollout, and rollback.

- Treat a clear user request or explicit approval as settling decisions it already
  answers.
- Use repository evidence to settle technical details that do not require a product
  choice.
- If no material decision remains, mark the plan settled and continue without a
  redundant question.
- If a material decision remains, invoke `$grill-me`. Ask one question at a time,
  include a recommendation, and do not edit until the answer settles the branch.

Do not restate the complete plan after every answer. Update only the decision that
changed and its dependents.

Gate output:

```text
Plan: <files/behavior/tests in a few lines>
Decision: settled | waiting on <one material question>
```

## Gate 3: Implement

Use RED -> GREEN -> REFACTOR for executable behavior changes:

1. Invoke `$go-testing-standards` and add or update a focused failing test.
2. Run the focused test and confirm it fails for the intended reason.
3. Make the smallest correct implementation change.
4. Run the focused and package tests, then refactor without changing behavior.

Load other skills only when their trigger applies:

- `$go-security-standards` for a changed trust boundary or security-sensitive path.
- `$go-struct-first` for loosely typed maps, context values, metadata, or parameter
  bags.
- `$code-structure` for proven cross-caller duplication or a necessary package
  boundary.

Record non-applicable implementation skills in one line. Do not load them merely to
prove they are not needed. Instruction, configuration, and generated-artifact-only
changes do not need a contrived failing unit test; validate the actual syntax or
consumer behavior instead.

## Gate 4: Verify and Report

Run focused checks while iterating. Run the full applicable suite once at handoff:

```bash
go fmt ./...
golangci-lint run ./...
go test -race -cover ./...
```

For concise output, run
`.agents/skills/go-implementation-workflow/scripts/verify.sh` from the repository
root. It stores full logs under `.context/agent-verification/` and prints only status
lines plus a bounded failure tail. Use `go test -v` only to diagnose a failure.

For frontend changes, also run:

```bash
npm --prefix web test -- --run
npm --prefix web run build
```

Report:

- Gate 1 evidence and justified N/A items
- the settled plan
- security considerations
- tests or direct validations added or changed
- commands and outcomes
- remaining risks or coverage gaps

Do not paste successful verbose logs, repeat complete skill text, or claim a check
passed when missing prerequisites prevented it from running.

## Token Discipline

- Reuse context already gathered in the current turn.
- Prefer targeted searches and file sections over whole-file dumps.
- Query an existing graph before rebuilding it.
- Load only the skill and reference required by the current gate.
- Run non-verbose focused tests during iteration and one full suite at handoff.
- Keep user updates to decisions, evidence, and outcomes rather than narrating every
  command.

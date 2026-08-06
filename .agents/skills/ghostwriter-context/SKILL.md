---
name: ghostwriter-context
description: Gather focused, revision-pinned context from the related Ghostwriter repository for Grimoire implementation, debugging, review, or compatibility work that touches internal/ghostwriter, internal/sync, GraphQL operations, service-token authentication, oplogs, or the Grimoire-Ghostwriter data contract. Use during Gate 1 of go-implementation-workflow; do not use for tasks whose behavior is fully local to Grimoire.
---

# Ghostwriter Context

Supply first-party Ghostwriter evidence to the Grimoire implementation workflow.
Keep the Ghostwriter checkout read-only.

## Establish the Source and Graph

From the Grimoire repository root, run:

```bash
.agents/skills/ghostwriter-context/scripts/ensure-graph.sh
```

The script resolves `GHOSTWRITER_REPO`, defaulting to
`~/dev/myrepos/Ghostwriter`, and records the exact Git commit. It reuses a valid
repo-local `graphify-out`, then a revision-keyed shared cache, and otherwise runs a
code-only Graphify build through the generic Graphify ensure script. Code-only
extraction is deterministic and does not spend semantic LLM tokens. The script does
not install or upgrade Graphify.

Treat a non-zero exit as a context blocker. Do not silently continue with a missing,
dirty, or ambiguous source tree.

## Gather Focused Evidence

1. Derive one focused cross-repository question from the Grimoire call site and the
   requested change.
2. Run `.agents/skills/graphify/scripts/query-compact.sh` against the absolute
   `GRAPHIFY_GRAPH` emitted by the script. Form the query from exact Ghostwriter
   symbols and vocabulary found at the Grimoire boundary; start with its default
   800-token budget. Load `$graphify` query references only for advanced expansion,
   path, explain, or result-feedback flows.
3. Verify important graph paths against the Ghostwriter source files and tests at
   `GRAPHIFY_SOURCE_ROOT`. Graph edges are static evidence, not runtime proof.
4. Read only documentation directly relevant to the contract under investigation.
5. Do not read Ghostwriter custom-agent prompts as implementation evidence. They
   govern development behavior in that repository; they are not architecture or API
   documentation.
6. Do not edit the Ghostwriter checkout. If the task requires Ghostwriter changes,
   report that cross-repository requirement and use a separate Ghostwriter Conductor
   workspace after the user settles the scope.

## Return the Gate 1 Handoff

Return only:

```text
Ghostwriter source: <absolute root and commit>
Graphify: <repo reuse | cache reuse | built, and absolute graph path>
Contract evidence: <files, symbols, tests, and confirmed behavior>
Compatibility risks: <none or concise list>
Open assumptions: <none or concise list>
```

Do not repeat Graphify's full report or paste broad source excerpts.

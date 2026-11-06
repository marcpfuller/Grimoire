---
name: ghostwriter-context
description: Gather focused, revision-pinned Ghostwriter context for Grimoire or Faultline work that depends on the Ghostwriter contract. Use during Gate 1 of implementation-workflow only from a Grimoire or Faultline workspace; never use while working directly in Ghostwriter.
---

# Ghostwriter Context

Supply first-party Ghostwriter evidence to a Grimoire or Faultline implementation
workflow. Keep the external Ghostwriter checkout read-only.

## Applicability Gate

Decide the repository boundary before resolving a source checkout or graph:

- Invoke this skill only when the active workspace is Grimoire or Faultline **and**
  the requested behavior depends on Ghostwriter code or its data contract.
- When the active workspace is Ghostwriter, do not invoke this skill. Inspect the
  local Ghostwriter workspace with the available language server and focused search;
  do not resolve `GHOSTWRITER_REPO`, query a Ghostwriter graph, or inspect an
  external Ghostwriter checkout.
- When the requested behavior is fully local to Grimoire or Faultline, do not invoke
  this skill.

Keywords such as GraphQL, service tokens, and Oplogs do not establish a
cross-repository boundary by themselves.

## Establish the Source and Graph

From the active Grimoire or Faultline repository root, run:

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

1. Derive one focused cross-repository question from the active Grimoire or
   Faultline call site and the requested change.
2. Run `.agents/skills/graphify/scripts/query-compact.sh` against the absolute
   `GRAPHIFY_GRAPH` emitted by the script. Form the query from exact Ghostwriter
   symbols and vocabulary found at the Grimoire or Faultline boundary; start with
   its default 800-token budget. Load `$graphify` query references only for advanced
   expansion, path, explain, or result-feedback flows.
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

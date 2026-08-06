---
name: graph-dep
description: Resolve and fetch version-matched third-party dependency source with the opensrc skill, then build or query a directed Graphify graph for that exact source. Use when the user asks to graph, map, visualize, query, or trace dependency internals, or when the mandatory implementation workflow determines that cross-source dependency relationships materially affect the change. Coordinate both skills and preserve package/version provenance.
---

# Graph Dependency Source

Coordinate `$opensrc` source resolution with deterministic Graphify scripts. Keep
dependency source read-only and derived graphs outside dependency caches.

## Define the Question

Derive from local project evidence:

- dependency distribution, module, or repository
- project call site and public entry symbol
- relationships needed, such as calls, imports, implementations, or data flow

Ask only when different entry points would materially change the investigation.

## Resolve Exact Source

Read `$opensrc` and let it identify the project-selected version, honor lockfiles and
Go `replace` directives, fetch source, and locate relevant implementation files.
Capture:

```text
dependency: <registry name or Go module path>
version: <exact selected version>
repository_ref: <repository tag or commit>
source_root: <absolute read-only source path>
project_call_site: <local file and symbol>
entry_symbols: <dependency symbols>
version_evidence: <lockfile, manifest, environment, or module metadata>
source_fallback: <none or explanation>
```

Do not continue without an existing absolute `source_root` and exact provenance.

## Ensure the Graph

From the Grimoire root, run:

```bash
.agents/skills/graph-dep/scripts/ensure-dependency-graph.sh \
  --source-root '<absolute source_root>' \
  --dependency '<dependency>' \
  --version '<version>' \
  --ref '<repository_ref>'
```

The script creates or reuses a provenance-checked code graph under
`/tmp/graph-dep-runs`, prevents concurrent duplicate builds, runs structural
diagnostics, stores verbose logs under `.context/graphify`, and prints a concise
handoff. It never writes into opensrc or Go module caches and never uses semantic LLM
tokens. Treat a non-zero exit as a blocker; do not perform half the workflow.

## Query and Verify

Build one focused query from exact entry symbols and graph vocabulary, then run:

```bash
.agents/skills/graphify/scripts/query-compact.sh \
  --graph '<GRAPHIFY_GRAPH>' \
  --budget 800 \
  '<focused query>'
```

Add `--context` filters or `--dfs` when local evidence supports them. Load the full
`$graphify` query reference only when the compact query has no useful match, requires
vocabulary expansion, or the user requests path, explain, feedback, or exports.

Verify important graph paths against dependency source locations. Distinguish static
edges from reflection, callbacks, generated dispatch, and runtime injection. Never
invent an edge or couple project code to a private dependency symbol without user
approval.

## Report

Summarize:

- dependency/module and exact version evidence
- repository ref and absolute read-only source root
- project call site and entry symbols
- graph status, absolute path, counts, warnings, and zero semantic run tokens
- relevant source files and confirmed behavior
- static-analysis limits and unresolved compatibility risks

Do not paste verbose build logs or the full graph report.

## Preserve Responsibilities

- `$opensrc` owns dependency identification, version resolution, fetching, and
  initial focused inspection.
- Graphify scripts own deterministic build/reuse, locking, diagnostics, bounded
  traversal, and logs.
- `$graph-dep` owns sequencing, provenance, source immutability, verification, and
  the combined handoff to `go-implementation-workflow`.

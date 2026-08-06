---
name: graphify
description: Build, update, or query a Graphify knowledge graph for code, documents, papers, images, or media. Use when the user explicitly asks for Graphify, a knowledge graph, graph traversal, graph visualization, or a query against an existing graphify-out graph, or when the mandatory implementation workflow selects Graphify for material cross-file context. Do not activate for ordinary focused codebase questions.
---

# Graphify Router

Load only the runbook required by the request.

## Route the Operation

- Routine code-only graph ensure or incremental update: run
  `scripts/ensure-code-graph.sh` without loading a build/update runbook.
- Existing graph plus a focused natural-language codebase question: run
  `scripts/query-compact.sh` with exact source symbols and a bounded budget.
- Vocabulary expansion, `path`, `explain`, or result feedback: read
  `references/query.md`.
- Semantic or explicitly customized new graph build: read
  `references/build-and-operations.md`, then load only the references it routes to.
- Semantic incremental update or clustering: read `references/update.md`.
- GitHub cloning or multi-repository merge: read `references/github-and-merge.md`.
- Video or audio transcription: read `references/transcribe.md`.
- Optional exports: read `references/exports.md`.
- `add` or watch mode: read `references/add-watch.md`.
- Hook or instruction-file integration: read `references/hooks.md`.

If no path is supplied for an explicit build, use the current directory. If
`graphify-out/graph.json` already exists, prefer querying it unless the user requests
an update or rebuild.

Common code-only commands:

```bash
.agents/skills/graphify/scripts/ensure-code-graph.sh --source-root .
.agents/skills/graphify/scripts/query-compact.sh \
  --graph graphify-out/graph.json --budget 800 '<focused query>'
```

The scripts keep full logs under `.context/graphify` and emit concise status or
bounded query output. Load the longer references only when the requested operation
cannot be represented by these interfaces.

## Installation Boundary

Check whether Graphify is available before a build. Do not silently install or
upgrade it. If unavailable, stop and ask before changing the environment or using
the network. Prefer an isolated `uv tool install graphifyy` installation when `uv`
is available; never use `--break-system-packages`.

## Context and Cost Discipline

- For focused code questions that local search answers, do not invoke Graphify.
- For an existing graph, load only the query runbook.
- For a code-only corpus, use deterministic AST extraction and skip semantic
  extraction.
- Use writable workers only when semantic extraction is necessary for documents,
  papers, or images.
- Preserve source paths and the `EXTRACTED`, `INFERRED`, and `AMBIGUOUS`
  distinctions. Never invent an edge.
- Surface graph-health warnings and token cost.

## Report

Report whether the graph was built, updated, or reused; the absolute output path;
the relevant query result or report sections; graph-health warnings; token usage;
and static-analysis limitations. Do not paste the entire graph report into chat.

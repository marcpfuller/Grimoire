---
name: graph-dep
description: Resolve and fetch version-matched third-party dependency source with the opensrc skill, then build or query a directed Graphify graph for that exact source. Use when the user asks to graph, map, visualize, query, or trace dependency internals, or when the mandatory implementation workflow determines that cross-source dependency relationships materially affect the change. Coordinate both skills and preserve package/version provenance.
---

# Graph Dependency Source

Coordinate `$opensrc` and `$graphify`: resolve the exact dependency source first, then run Graphify against that immutable source tree from a separate writable analysis directory.

## Load Both Skills

Before taking task actions, read both `SKILL.md` files completely and follow any references they route the current operation to.

Load `$opensrc` and `$graphify` from the active repo skill catalog. If either skill
is unavailable, stop before fetching and report the missing skill. Do not fall back
to machine-specific paths, guess Graphify commands, or perform only half the
workflow unless the user requests a partial result.

## Define the Graph Target

Derive a focused question from the request and local project evidence:

- Dependency distribution, module, or repository
- Project call site or import boundary
- Public dependency entry symbol
- Required relationships, such as calls, imports, implementations, inheritance, ownership, or data flow
- Requested Graphify export flags or output format

Infer the entry symbol from the local call site when possible. Ask only when different entry points would materially change the graph and the code cannot resolve the choice.

## Resolve Source with opensrc

Use `$opensrc` to identify the registry package or Go module, establish the project-selected version, honor lockfiles and Go `replace` directives, fetch source, and identify the likely implementation files.

Capture this handoff:

```text
dependency: <registry name or Go module path>
version: <exact selected version>
repository_ref: <repository and tag or commit>
source_root: <absolute read-only source path>
project_call_site: <local file and symbol>
entry_symbols: <dependency symbols>
version_evidence: <lockfile, manifest, environment, or module metadata>
source_fallback: <none or Go module-cache explanation>
```

Require `source_root` to be an existing absolute directory. Treat it as read-only. Do not create `graphify-out/` inside the opensrc cache or Go module cache.

Do not stop after fetching. Continue to Graphify unless a required precondition fails.

## Create a Dedicated Graph Workspace

Graphify writes `graphify-out/` relative to its current working directory. Keep those derived artifacts separate from both the user project and dependency cache.

Create a deterministic run directory under:

```text
/tmp/graph-dep-runs/<dependency>-<version>/
```

Sanitize the final directory name to letters, digits, `.`, `_`, and `-`. Resolve the exact path before creating or writing it; never use an unresolved variable or broad `/tmp` path as a destructive target.

Run the Graphify workflow with the run directory as the current working directory and `source_root` as `INPUT_PATH`. Write derived artifacts to:

```text
<run-directory>/graphify-out/
```

## Reuse a Matching Graph

Before rebuilding, check the run directory for:

```text
graphify-out/graph.json
graphify-out/.graphify_root
```

Reuse the graph only when `.graphify_root` resolves to the same `source_root` and the dependency version/ref matches the handoff record.

- For a natural-language question, follow Graphify's existing-graph fast path and run `graphify query "<question>"`.
- For a requested traversal, use Graphify's documented `query`, `path`, or `explain` flow.
- For an explicit refresh of the same source tree, follow Graphify's `--update` flow.
- For a different version or ref, create a different deterministic run directory and build a new graph.

Do not reuse a graph solely because the package name matches.

## Build with Graphify

For a new graph, invoke the Graphify workflow conceptually as:

```text
/graphify <absolute-source-root> --directed
```

Use `--directed` by default so call and import direction remains source-to-target. Add `--mode deep`, `--no-viz`, `--svg`, `--graphml`, `--neo4j`, `--falkordb`, `--wiki`, `--obsidian`, or other Graphify flags only when the user requests them or they are necessary for the deliverable.

Follow Graphify's complete ordered pipeline rather than replacing it with an ad hoc parser:

1. Resolve the Graphify Python interpreter.
2. Detect and summarize the corpus.
3. Apply Graphify's corpus-size warning and narrowing rules.
4. Perform deterministic AST extraction for code.
5. Perform semantic extraction only for detected docs, papers, or images, following Graphify's routing rules.
6. Merge extraction results and build the directed graph.
7. Run the graph health check.
8. Label communities and generate the report.
9. Generate HTML unless `--no-viz` was requested.
10. Save the manifest, cost record, and final outputs.

For a code-only dependency corpus, use Graphify's AST fast path and empty semantic extraction file. Do not request any API key. Graphify does not need an API key for code extraction.

If Graphify is not installed, follow its interpreter detection instructions. Installing or upgrading `graphifyy` changes the environment and may use the network, so obtain any approval required by the host before installation.

## Scope the Corpus Honestly

Pass the whole resolved source root by default so Graphify can preserve cross-directory relationships. Allow Graphify's detection threshold to identify packages too large to process comfortably.

If Graphify reports more than 500 files or 2,000,000 words, show its required warning and use its first-level-directory analysis. Narrow only with the user's choice when Graphify requires that choice.

Exclude or narrow unrelated vendored, generated, minified, fixture, benchmark, and build-output areas only through Graphify-supported behavior or by choosing an appropriate source subdirectory. Do not mutate the dependency tree to reduce scope.

## Validate and Preserve Provenance

Treat the graph as a derived artifact rather than independent truth.

- Surface every Graphify graph-health warning.
- Preserve Graphify's `EXTRACTED`, `INFERRED`, and `AMBIGUOUS` distinctions.
- Never invent an edge.
- Verify important paths against source locations when answering a question.
- Distinguish static edges from reflection, callbacks, dependency injection, generated dispatch, and other runtime behavior.
- Preserve dependency, version, repository ref, source root, and project call-site metadata in the combined report.

## Deliver the Result

Report the absolute run directory and Graphify outputs:

```text
graphify-out/graph.html
graphify-out/GRAPH_REPORT.md
graphify-out/graph.json
```

Include optional outputs only when their flags were requested. Follow Graphify's reporting requirements: provide token cost, surface health warnings, paste only the God Nodes, Surprising Connections, and Suggested Questions sections, and offer to trace the most interesting suggested question.

Also summarize:

- Dependency/module and exact version
- Version evidence and repository ref
- Source root and Graphify run directory
- Project call site and dependency entry symbols
- Whether the graph was built, updated, or reused
- Whether a Go module-cache fallback was used
- Static-analysis limitations and unresolved dynamic behavior

## Preserve Responsibilities

- Let `$opensrc` own dependency identification, version resolution, fetching, and initial source inspection.
- Let `$graphify` own detection, extraction, graph construction, clustering, exports, graph queries, health checks, and Graphify-specific reporting.
- Let `$graph-dep` own sequencing, the read-only-source boundary, run-directory isolation, provenance handoff, reuse decisions, and combined reporting.

Re-read the installed underlying skills on every invocation so improvements to either workflow are inherited automatically.

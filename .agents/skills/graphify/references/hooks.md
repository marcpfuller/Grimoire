# graphify reference: Git hooks and native CLAUDE.md integration

Load this when the user asked to install Git hooks or wire graphify into a project's CLAUDE.md.

## For Git hooks

Install post-commit and post-checkout hooks that auto-rebuild the graph after
commits and primary-checkout branch switches. Rebuilds run detached with no LLM.

```bash
graphify hook install    # install
graphify hook uninstall  # remove
graphify hook status     # check
```

After every `git commit`, the hook detects which code files changed (via `git diff HEAD~1`), re-runs AST extraction on those files, and rebuilds `graph.json` and `GRAPH_REPORT.md`. A branch switch triggers a full code rebuild. Doc/image changes are ignored by the hook - run `/graphify --update` manually for those.

If a post-commit hook already exists, graphify appends to it rather than replacing it.

Graphify deliberately skips both hooks in linked Git worktrees. Conductor uses
linked worktrees, so keep queries fresh there with `query-compact.sh`, which runs
`ensure-code-graph.sh` immediately before traversal. Also note that hook installation
registers a merge driver and may edit tracked `.gitattributes`; inspect that change
when the graph is intended to remain local-only.

---

## For native CLAUDE.md integration

Run once per project to make graphify always-on in Claude Code sessions:

```bash
graphify claude install
```

This writes a `## graphify` section to the local `CLAUDE.md` that instructs Claude to check the graph before answering codebase questions and rebuild it after code changes. No manual `/graphify` needed in future sessions.

```bash
graphify claude uninstall  # remove the section
```

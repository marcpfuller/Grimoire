---
name: opensrc
description: Use opensrc to inspect version-matched source code for third-party dependencies used by a project. Use when implementing, debugging, reviewing, or explaining behavior that depends on an npm, PyPI, crates.io, Go module, GitHub, GitLab, or Bitbucket package; when documentation and type declarations do not explain runtime behavior; or when the user asks how a dependency works internally. Fetch only dependencies relevant to the current task.
---

# opensrc Dependency Source Context

Use `opensrc` to inspect dependency implementations when project code, public APIs, documentation, and type declarations are insufficient.

## Follow the Core Rules

- Inspect the project before fetching source.
- Fetch only packages directly relevant to the task; do not fetch every dependency.
- Match the version selected by the project whenever possible.
- Search fetched source selectively with `rg` when available, or a focused
  `find`/`grep` fallback; do not load whole repositories into context.
- Treat dependency source as read-only reference material.
- Do not execute scripts or binaries from fetched repositories.
- Do not copy substantial dependency code into the project.
- Do not alter the opensrc cache unless the user requests cache maintenance.

## Decide Whether Source Is Needed

1. Read the local call site, imports, configuration, tests, and error output.
2. Inspect the dependency's public types or documented interface.
3. Form a precise implementation question.
4. Fetch source only when the answer depends on internal behavior.

Use source to answer questions such as:

- Where is this public symbol implemented?
- How are arguments validated and defaults applied?
- Which errors can this path return?
- Does this operation mutate, cache, retry, serialize, or perform I/O?
- Which supported extension point fits the project?
- Did behavior change between dependency versions?

Do not fetch source for routine API usage already answered by local types or documentation.

## Identify the Dependency and Version

Trace the relevant import, stack frame, or public symbol to its distribution or module. Inspect the nearest applicable files:

- JavaScript: `package.json`, `package-lock.json`, `pnpm-lock.yaml`, or `yarn.lock`
- Python: `requirements/*.txt`, `pyproject.toml`, `poetry.lock`, `uv.lock`, or the active environment
- Rust: `Cargo.toml` and `Cargo.lock`
- Go: `go.mod`, `go.sum`, `go.work`, and `vendor/modules.txt`

Account for import names that differ from registry package names. Prefer versions in this order:

1. Exact lockfile-selected version
2. Exact installed version
3. Manifest constraint
4. Latest version only when no project version can be established

State explicitly when the inspected version is not proven to match the project.

## Verify opensrc

Run:

```bash
command -v opensrc
opensrc --version
```

If unavailable, tell the user that it can be installed with:

```bash
npm install -g opensrc
```

Do not install software silently. Request permission when installation, network access, or writing the cache requires approval. Do not silently substitute another source-fetching tool.

## Resolve Source

Use `opensrc path` to fetch on a cache miss and print the cached source path.

### npm

Pass the directory controlled by the relevant lockfile:

```bash
dependency_src="$(opensrc path '<package>' --cwd '<project-directory>')"
```

Specify a version when lockfile resolution is unavailable:

```bash
dependency_src="$(opensrc path '<package>@<version>')"
```

### PyPI

Read the exact version from project requirements or the active environment:

```bash
python -m pip show '<distribution>'
dependency_src="$(opensrc path 'pypi:<distribution>@<version>')"
```

Do not assume the Python import name equals the PyPI distribution name.

### crates.io

Read the selected version from `Cargo.lock`:

```bash
dependency_src="$(opensrc path 'crates:<crate>@<version>')"
```

### Go modules

Recognize that opensrc has no native Go registry resolver. Resolve the exact Go module, version, and backing repository with the Go toolchain, then fetch that repository through opensrc.

Identify the module providing the imported package:

```bash
go list -m -json all
go list -deps -json ./...
```

Resolve a known module and download metadata:

```bash
go list -m -json '<module-path>'
go mod download -json '<module-path>@<version>'
```

Inspect these fields when present:

- `Path`: canonical module path
- `Version`: selected module version
- `Replace`: alternate module or local replacement
- `Dir`: exact source directory in the Go module cache
- `Origin.URL`: upstream version-control repository
- `Origin.Ref` or `Origin.Hash`: resolved source reference

Always honor `replace` directives. Inspect a local replacement directly instead of fetching its public upstream repository.

When metadata identifies a GitHub, GitLab, or Bitbucket repository, normalize it to opensrc syntax and preserve the selected tag or commit:

```bash
# GitHub
dependency_src="$(opensrc path '<owner>/<repository>@<tag-or-commit>')"

# GitLab
dependency_src="$(opensrc path 'gitlab:<owner>/<repository>@<tag-or-commit>')"

# Bitbucket
dependency_src="$(opensrc path 'bitbucket:<owner>/<repository>@<tag-or-commit>')"
```

Example:

```bash
go mod download -json 'github.com/stretchr/testify@v1.9.0'
dependency_src="$(opensrc path 'stretchr/testify@v1.9.0')"
```

Do not infer vanity-module repositories from their domains. Resolve module paths such as `golang.org/x/...`, `go.uber.org/...`, `gorm.io/...`, and company domains from Go metadata or authoritative module information.

Handle Go version details carefully:

- Preserve semantic versions and `+incompatible` suffixes.
- Recognize `/v2` and later major-version module paths.
- For pseudo-versions, fetch the underlying commit if no matching tag exists.
- For modules in monorepo subdirectories, check whether the tag includes the subdirectory prefix.

If no reliable repository/ref mapping exists, inspect the exact module-cache source:

```bash
dependency_src="$(go list -m -f '{{.Dir}}' '<module-path>')"
```

Report this as a fallback where opensrc could not reliably resolve the module repository. Do not pretend that opensrc performed the fetch.

### Hosted repositories

Pin a tag, branch, or commit when known:

```bash
dependency_src="$(opensrc path '<owner>/<repository>@<ref>')"
dependency_src="$(opensrc path 'gitlab:<owner>/<repository>@<ref>')"
dependency_src="$(opensrc path 'bitbucket:<owner>/<repository>@<ref>')"
```

Use an unpinned repository only when the task concerns its current development branch rather than a released dependency.

## Confirm and Search the Source

Confirm the resolved directory before reading it. Prefer `rg`, but do not install it
just for this workflow; use `find` and `grep` when it is unavailable.

```bash
test -d "$dependency_src"
if command -v rg >/dev/null 2>&1; then
    rg --files "$dependency_src" | head -80
else
    find "$dependency_src" -type f | head -80
fi
```

Search for the exact public symbol, error text, or configuration key:

```bash
rg -n '<function|class|method|type|error>' "$dependency_src"
rg -n '<error text or configuration key>' "$dependency_src"
rg -n --glob '*test*' --glob '*spec*' '<symbol>' "$dependency_src"
rg -n --glob '*_test.go' '<Go symbol>' "$dependency_src"
```

Follow the implementation chain:

1. Public export or package entry point
2. Function, method, class, interface, or receiver implementation
3. Internal helpers called by that implementation
4. Configuration, defaults, and platform-specific behavior
5. Tests covering the relevant edge case

Exclude vendored dependencies, generated code, minified files, fixtures, and build output unless they control the behavior under investigation. Read focused sections of matching files rather than dumping entire files.

## Apply the Evidence

Connect the evidence before changing project code:

```text
local call site
    -> dependency public API
    -> internal implementation
    -> confirmed behavior or edge case
    -> project change
```

Distinguish confirmed source behavior from inference and unresolved assumptions. Prefer supported public APIs. Do not couple project code to private internals unless the user explicitly accepts that maintenance risk.

After changing code:

- Run the relevant project tests.
- Add a regression test when practical.
- Cover error paths or edge cases discovered in the dependency source.
- Avoid tests that directly depend on private dependency symbols.

## Report Findings

Summarize:

- Package or module name and inspected version
- How the version was established
- Source repository/ref and resolved local path
- Relevant implementation files and symbols
- Behavior confirmed by the source
- Effect on the solution
- Any version mismatch or unresolved uncertainty
- For Go, the module path, selected version, repository/ref, and applicable `replace` directive

Reference specific files and line numbers when possible. Do not present cached dependency files as files modified in the user's project.

## Inspect the Cache Safely

Use only when needed:

```bash
opensrc list
opensrc list --json
```

Do not run `opensrc remove` or `opensrc clean` unless the user explicitly asks to delete cached sources.

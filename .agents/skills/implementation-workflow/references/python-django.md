# Python and Django Implementation Guidance

Apply this profile alongside the shared implementation gates for changes that
materially affect Python, Django, migrations, or Python build behavior.

## Tool Readiness

Inspect repository instructions and configuration before choosing commands:

- Python version and environment or container boundary
- dependency manifests and lockfiles
- Django settings and test runner
- formatter, linter, and type-checker configuration
- Docker Compose services when the project requires container execution

Do not create a new virtual environment, install packages, start shared services, or
upgrade tools silently. Use the project's documented execution boundary even when a
host command happens to be available.

## Source Context

Use an available Python language server for definitions, references, symbols, and
diagnostics. When Serena/Pyright is available, collect diagnostics for each Python
source file before editing it and again after editing it. If whole-file output is
dominated by baseline findings, query the changed symbols or line range and resolve
every new error before handoff. Use focused `rg` searches for exact values,
templates, migrations, settings, generated files, or when semantic tooling cannot
answer. Inspect complete Django model, manager, form, serializer, view, task, and
signal definitions involved in the path before planning a new boundary.

Treat migrations, settings, templates, JavaScript callers, asynchronous workers,
and database constraints as source context when they affect the behavior. For a
polyglot boundary, read the other applicable profile rather than assuming the
Python call site owns the entire contract.

## Implementation and Verification

Use the Python/Django profile from `$testing-standards`. Invoke
`$typed-state-first` before reading values from JSON, session data, serializer
inputs, metadata dictionaries, or other loose containers that affect behavior. At
an untrusted boundary, validate the runtime object shape before indexing it; a type
hint, `cast()`, or an invariant established only in another method is not runtime
validation.

Run the exact commands established by repository instructions. A typical handoff
may include:

- focused and app-level Django tests
- the applicable full Django suite
- migration checks when models change
- configured formatting and import sorting on touched files
- configured lint and type checks
- frontend checks when a Python change alters a browser or GraphQL contract

In Ghostwriter, use its Docker Compose test boundary and existing Django unittest
style unless current repository evidence says otherwise. Use mypy with django-stubs
for project type verification when configured. Serena/Pyright diagnostics are
required for changed Python source when available, but do not replace the
repository's configured type-check command.

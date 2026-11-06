---
name: testing-standards
description: Apply pragmatic test-driven development, stable test boundaries, meaningful assertions, concurrency checks, and risk-based coverage. Use when changing behavior, writing or reviewing tests, debugging test failures, or deciding appropriate verification.
---

# Testing Standards

## Select Language Guidance

Read only the profile matching the behavior under test:

- Go: [references/go.md](references/go.md)
- Python or Django: [references/python-django.md](references/python-django.md)
- A materially cross-language behavior: read each affected profile.

For another language, apply the shared standards using the repository's established
framework and commands.

## Choose the Test Boundary

Test observable behavior at the narrowest stable boundary:

- pure function for deterministic logic
- public module or package API for domain behavior
- framework request harness for transport behavior
- temporary directory or test database for persistence
- integration or end-to-end test only when component interaction is the behavior

Do not test private implementation details solely to increase coverage. A test seam
that cannot reproduce the real behavior gives false confidence.

## RED -> GREEN -> REFACTOR

For an executable behavior change:

1. Add or update a focused test that fails for the intended reason.
2. Run that test and record the failure.
3. Implement the smallest correct change.
4. Run the focused test, then the relevant module or package tests.
5. Refactor without changing behavior.
6. Run formatting, lint, concurrency, and applicable full-suite checks.

Documentation, instructions, generated output, or configuration without executable
behavior does not need a contrived failing unit test. Validate the artifact through
its actual parser or consumer and state why no behavior test was added.

## Assertions and Fixtures

- Prefer a direct test when only one behavior is involved; parameterize genuinely
  repeated cases.
- Fail immediately when later assertions depend on setup succeeding.
- Assert externally meaningful values, states, and error categories. Avoid brittle
  assertions on irrelevant formatting or internal implementation details.
- Never ignore setup or cleanup failures.
- Keep fixtures local to the test that owns them unless sharing removes substantial
  duplication without obscuring intent.
- Isolate external services at their boundary, but do not mock the behavior under
  test.

## Security and Adversarial Cases

For untrusted input, cover applicable empty, malformed, boundary, oversized, and
adversarial cases. Test missing, invalid, and wrong-scope authorization separately.
Security-critical branches should have complete practical coverage.

## Concurrency and Time

- Use the ecosystem's race or concurrency checks when shared state, asynchronous
  workers, threads, processes, or event loops are involved.
- Prefer synchronization primitives, controllable clocks, or eventual assertions
  over fixed sleeps.
- Give asynchronous tests bounded timeouts.
- Do not parallelize tests that share process state, environment variables, fixed
  ports, or a database unless isolation is proven.

## Coverage and Reporting

Coverage is a diagnostic, not a universal percentage gate. Focus on changed
behavior, error paths, and trust boundaries. Report flaky, skipped, and
environment-dependent tests explicitly.

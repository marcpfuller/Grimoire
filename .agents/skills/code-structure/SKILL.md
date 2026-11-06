---
name: code-structure
description: Design or refactor module and package boundaries when multiple workflows duplicate operational mechanics or a feature needs a stable seam. Use for real cross-caller duplication and responsibility-boundary decisions; do not activate for single-use logic or cosmetic reorganization.
---

# Code Structure

Separate domain orchestration from reusable mechanics without introducing layers
that the codebase does not need.

## Start With Evidence

Before proposing an abstraction:

1. Trace the current callers, interfaces, implementations, tests, and errors.
2. Identify the repeated block and list its callers.
3. Separate domain policy from operational mechanics.
4. Confirm that extraction removes meaningful duplication or creates a stable seam
   required by the new feature.

Reuse or adapt an existing abstraction when it already provides the required data
or mechanics. Do not introduce a parallel helper solely for a narrower result or a
more convenient shape. Add one only when the existing path cannot be reused safely
or correctly, and state that limitation before implementation.

Keep logic local when it has one caller, is domain-specific, or is likely to change
with that workflow.

## Ownership

Domain orchestration owns:

- authorization and business rules
- state transitions and ordering
- retry and failure policy
- user-visible error classification
- deciding when an operation occurs

Reusable mechanics own:

- protocol or filesystem operations
- serialization and transformation
- starting or stopping a runtime resource
- interaction with a third-party client
- deterministic computation shared by callers

Do not move policy into a generic service merely to shorten a caller.

## Language Profile

Read only the profile matching the boundary being changed:

- For Go, read [references/go.md](references/go.md).
- For Python or Django, read
  [references/python-django.md](references/python-django.md).
- For a boundary that materially spans both, read both profiles.

For another language, apply the shared workflow and inspect that project's existing
conventions before proposing an idiomatic boundary. Do not force a profile from a
different language onto it.

## Migration

1. Capture current behavior with focused tests.
2. Extract the smallest repeated mechanic.
3. Migrate one caller and verify it.
4. Migrate remaining callers incrementally.
5. Remove the old path only after every caller is covered.
6. Re-run the project's focused tests, concurrency checks where applicable, and
   lint or type checks.

Avoid god services, speculative interfaces, generic parameter bags, import cycles,
and broad refactors that mix behavior changes with file movement.

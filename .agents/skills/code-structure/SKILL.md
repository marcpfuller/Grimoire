---
name: code-structure
description: Design or refactor Go package boundaries when multiple workflows duplicate operational mechanics or a new feature shares established mechanics. Use for real cross-caller duplication and service-boundary decisions; do not activate for single-use logic or cosmetic reorganization.
---

# Go Code Structure

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

## Go API Shape

- Put the abstraction in the package that owns the behavior, not a generic
  `utils` package.
- Accept explicit typed inputs and return typed results.
- Define interfaces at the consumer boundary when substitution is required.
- Keep interfaces narrow and driven by actual callers.
- Return or wrap errors with operation context; do not swallow them.
- Avoid hidden global state and package initialization side effects.

Example:

```go
type CommandStore interface {
	CreateCommand(ctx context.Context, input CreateCommandInput) (Command, error)
}

type Recorder struct {
	store CommandStore
}

func (r *Recorder) Record(ctx context.Context, input RecordInput) (Command, error) {
	if err := validateRecord(input); err != nil {
		return Command{}, err
	}
	cmd, err := r.store.CreateCommand(ctx, toCreateInput(input))
	if err != nil {
		return Command{}, fmt.Errorf("create command: %w", err)
	}
	return cmd, nil
}
```

The caller retains validation and workflow policy; the store owns persistence.

## Migration

1. Capture current behavior with focused tests.
2. Extract the smallest repeated mechanic.
3. Migrate one caller and verify it.
4. Migrate remaining callers incrementally.
5. Remove the old path only after every caller is covered.
6. Re-run package tests, race checks where concurrency is involved, and lint.

Avoid god services, speculative interfaces, generic parameter bags, import cycles,
and broad refactors that mix behavior changes with file movement.

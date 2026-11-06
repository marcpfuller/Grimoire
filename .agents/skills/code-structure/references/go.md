# Go Boundary Guidance

Use these rules after the shared workflow establishes that a Go package boundary
should change.

## API Shape

- Put the abstraction in the package that owns the behavior, not a generic
  `utils` package.
- Accept explicit typed inputs and return typed results.
- Define interfaces at the consumer boundary only when substitution is required.
- Keep interfaces narrow and driven by actual callers.
- Return or wrap errors with operation context; do not swallow them.
- Avoid hidden global state and package initialization side effects.

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

The workflow caller retains validation, authorization, ordering, and retry policy;
the store owns persistence mechanics.

## Verification

Run focused package tests while iterating. At handoff, run the repository's
applicable formatter, linter, test suite, and race checks for concurrency-sensitive
changes.

---
name: go-testing-standards
description: Apply pragmatic Go TDD, table-driven testing, error assertions, race checks, and risk-based coverage. Use when changing Go behavior, writing or reviewing Go tests, debugging test failures, or deciding appropriate verification.
---

# Go Testing Standards

## Choose the Test Boundary

Test observable behavior at the narrowest stable boundary:

- pure function for deterministic logic
- package API for domain behavior
- `httptest` for HTTP handlers
- temporary directory or test database for persistence
- integration test only when component interaction is the behavior

Do not test private implementation details solely to increase coverage.

## RED -> GREEN -> REFACTOR

For an executable behavior change:

1. Add or update a focused test that fails for the intended reason.
2. Run that test and record the failure.
3. Implement the smallest correct change.
4. Run the focused test, then the package tests.
5. Refactor without changing behavior.
6. Run formatting, lint, race, and applicable full-suite checks.

A contrived failing test is not required for documentation, instructions,
generated output, or configuration that has no executable behavior. Validate those
artifacts directly and state why no test was added.

## Test Shape

Use table-driven tests when several cases share setup and assertions:

```go
func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "valid", input: "operator"},
		{name: "empty", input: "", wantErr: ErrNameRequired},
		{name: "too long", input: strings.Repeat("a", 256), wantErr: ErrNameTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateName() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
```

Prefer a direct test when only one behavior is involved. Mark helpers with
`t.Helper()`, use `t.Cleanup`, and keep fixtures local to the test that owns them.

## Errors and Assertions

- Use `errors.Is` and `errors.As` for wrapped errors.
- Fail immediately when later assertions depend on setup succeeding.
- With Testify, use `require` for prerequisites and `assert` for independent
  comparisons.
- Assert externally meaningful error classes or messages; avoid brittle full-string
  comparisons for internal wrapped errors.
- Never ignore an error in test setup.

## HTTP and Security Cases

Handler tests should cover method, path, status, content type, body shape, and
authentication/authorization behavior as applicable:

```go
req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
rr := httptest.NewRecorder()
handler.ServeHTTP(rr, req)

if rr.Code != http.StatusUnauthorized {
	t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
}
```

For untrusted input, include empty, malformed, boundary, oversized, and adversarial
cases. Security-critical branches should have complete practical coverage.

## Concurrency

- Run `go test -race` for code involving goroutines, channels, shared state, PTYs,
  event brokers, or background synchronization.
- Prefer synchronization primitives or eventual assertions over `time.Sleep`.
- Give asynchronous tests bounded timeouts so failures terminate predictably.
- Do not call `t.Parallel()` when tests share process-wide state, environment,
  fixed ports, or a database.

## Build Tags

Use the current build constraint syntax:

```go
//go:build integration

package example
```

Keep tagged integration tests runnable with a documented command.

## Coverage and Commands

Coverage is a diagnostic, not a universal gate. Focus on changed behavior, error
paths, and security boundaries rather than forcing unrelated packages above an
arbitrary percentage.

```bash
go test ./internal/api -run TestHandleName -count=1
go test ./internal/api
go test -race -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Use `-count=1` when cached results would hide timing or state issues. Report flaky,
skipped, and environment-dependent tests explicitly.

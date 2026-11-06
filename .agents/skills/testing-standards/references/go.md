# Go Testing Guidance

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

Mark helpers with `t.Helper()`, use `t.Cleanup`, and keep fixtures local. Use
`errors.Is` and `errors.As` for wrapped errors. With Testify, use `require` for
prerequisites and `assert` for independent comparisons.

## HTTP and Concurrency

Use `httptest` for handler behavior and assert method, path, status, content type,
body shape, and authorization as applicable.

Run `go test -race` for goroutines, channels, shared state, PTYs, event brokers, or
background synchronization. Do not call `t.Parallel()` when tests share process
state, environment, fixed ports, or a database.

Use current build constraints for tagged integration tests:

```go
//go:build integration

package example
```

## Commands

```bash
go test ./internal/api -run TestHandleName -count=1
go test ./internal/api
go test -race -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Use `-count=1` when cached results would hide timing or state problems.

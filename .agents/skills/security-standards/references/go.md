# Go Security Guidance

Use Go's standard library or established project dependencies for the controls
selected by the shared threat model.

## Input, Secrets, and Authorization

Limit request bodies before decoding and reject unknown JSON fields when the
contract requires it:

```go
body := http.MaxBytesReader(w, r.Body, 1<<20)
dec := json.NewDecoder(body)
dec.DisallowUnknownFields()
if err := dec.Decode(&input); err != nil {
	http.Error(w, "invalid request", http.StatusBadRequest)
	return
}
```

Use private context-key types and checked assertions. Compare equal-length opaque
tokens with `subtle.ConstantTimeCompare`. Use `%w` to retain internal error context,
but never serialize a sensitive error chain to an untrusted client. Library packages
must not terminate the process with `log.Fatal`.

## SQL and Commands

Use parameterized SQL and check row iteration, scan, transaction, commit, and
rollback errors. Use `exec.CommandContext` with separate arguments and a deadline;
do not interpolate input into `sh -c` or `bash -c`.

```go
cmd := exec.CommandContext(ctx, binary, "--config", configPath)
cmd.Env = minimalEnvironment()
output, err := cmd.CombinedOutput()
if err != nil {
	return fmt.Errorf("run helper: %w", err)
}
```

Do not expose captured output when it may contain secrets.

## Filesystem

Prefer capability-based access with `os.Root` on supported Go versions:

```go
root, err := os.OpenRoot(baseDir)
if err != nil {
	return nil, fmt.Errorf("open root: %w", err)
}
defer root.Close()

f, err := root.Open(userPath)
if err != nil {
	return nil, errors.New("invalid path")
}
defer f.Close()
```

When only lexical validation is possible, reject absolute paths and any
`filepath.Rel` result equal to `..` or beginning with `..` plus a separator. Resolve
symlink escape separately. Use `os.CreateTemp` or `os.MkdirTemp` for temporary data.

## Cryptography and Services

Use `crypto/rand`, never `math/rand`, for security values. Use AEAD such as AES-GCM
or ChaCha20-Poly1305 with a fresh nonce of the required size. Use
`crypto/rand.Text()` only when its fixed security properties meet the protocol.

Configure HTTP read-header, read, write, and idle timeouts. Bind local-only services
to loopback, set explicit response content types, use a deliberate CSP for browser
content, and apply rate or concurrency limits where abuse is plausible.

Run `go test -race` for shared or concurrent state and apply the Go profile from
`$testing-standards` to security cases.

---
name: go-security-standards
description: Threat-model and implement secure Go boundaries involving untrusted input, authentication, authorization, secrets, cryptography, SQL, HTTP, filesystem access, command execution, or sensitive logging. Use for security-sensitive implementation and review, not for unrelated internal refactors.
---

# Go Security Standards

Apply controls based on the actual trust boundary and threat model. Do not add
security theater or copy generic middleware without checking the deployment model.

## Threat Model First

For each changed boundary, identify:

- attacker-controlled inputs and their maximum size
- the authoritative identity and authorization decision
- secrets or sensitive terminal data in memory, storage, errors, and logs
- filesystem, database, network, and process capabilities reached by the input
- denial-of-service, replay, race, and confused-deputy risks
- the external error contract and the internal diagnostic path

Document controls that are not applicable and why when the task is security-critical.

## Input Validation

- Enforce size limits before decoding, allocation, regex work, decompression, or
  database access.
- Validate type, range, format, enumeration membership, and cross-field invariants.
- Normalize only when the protocol defines equivalent forms. Validate the normalized
  value actually used by the operation.
- Reject unknown fields when silent acceptance could hide a client error or bypass a
  policy.
- Use request-scoped deadlines and bounded readers for network input.

```go
const maxBody = 1 << 20

body := http.MaxBytesReader(w, r.Body, maxBody)
dec := json.NewDecoder(body)
dec.DisallowUnknownFields()
if err := dec.Decode(&input); err != nil {
	http.Error(w, "invalid request", http.StatusBadRequest)
	return
}
```

Validate domain values separately after decoding.

## Authentication and Authorization

- Authenticate credentials before trusting identity claims.
- Authorize every protected operation against the requested resource; authentication
  alone is not authorization.
- Deny by default and use the same externally visible failure where distinguishing
  cases would leak account or resource existence.
- Validate token algorithm, signature, issuer, audience, expiry, and not-before as
  required by the protocol. Do not accept an algorithm selected only by untrusted
  token metadata.
- Use established password hashing such as bcrypt or Argon2id with project-approved
  parameters. Never store plaintext passwords.
- Compare opaque secret tokens in constant time after checking expected lengths.

```go
func equalToken(got, want []byte) bool {
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
```

Use private context key types and checked type assertions. Do not treat a context
value as authoritative unless trusted middleware set it.

## Secrets, Errors, and Logging

- Load secrets from an approved secret source or environment; never commit them.
- Return an error when required configuration is absent. Library packages should not
  terminate the process with `log.Fatal`.
- Do not log passwords, tokens, cookies, authorization headers, encryption keys,
  raw terminal output containing credentials, or complete sensitive request bodies.
- Prefer allowlisted structured fields over broad object dumps.
- Return stable generic errors across external boundaries. Wrap internal errors with
  operation context using `%w`, while ensuring the detailed chain is not serialized
  to an untrusted client.

## SQL and Process Execution

- Use parameterized SQL for values. Identifiers cannot be parameterized; select them
  from a fixed allowlist rather than concatenating user input.
- Check transaction, row iteration, scan, commit, and rollback errors.
- Avoid invoking a shell. Use `exec.CommandContext` with separate arguments and a
  deadline when Grimoire itself starts a process.
- Never interpolate untrusted values into `sh -c`, `bash -c`, or a command string.
- Keep captured user commands as data. Do not accidentally re-execute captured text.

```go
cmd := exec.CommandContext(ctx, binary, "--config", configPath)
cmd.Env = minimalEnvironment()
output, err := cmd.CombinedOutput()
if err != nil {
	return fmt.Errorf("run helper: %w", err)
}
```

Do not include sensitive command output in returned errors or logs.

## Filesystem Access

Prefer capability-based access with `os.Root` on supported Go versions so symlinks
and traversal cannot escape the intended directory:

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

When only lexical validation is possible, reject absolute paths and any `filepath.Rel`
result equal to `..` or beginning with `..` plus a separator. A string prefix check is
not containment and remains vulnerable to sibling prefixes. Lexical checks also do
not prevent symlink escape; resolve that threat separately.

- Create secret-bearing files with restrictive permissions from the start.
- Avoid predictable temporary names; use `os.CreateTemp` or `os.MkdirTemp`.
- Limit upload size before reading, detect content from bytes, check read/seek errors,
  and do not trust filename extensions or client MIME headers.

## Cryptography and Randomness

- Use `crypto/rand`, never `math/rand`, for tokens, nonces, or security identifiers.
- Use authenticated encryption such as AES-GCM or ChaCha20-Poly1305. Do not use CFB,
  CBC, or CTR without a separate correctly composed authenticator.
- Use a fresh nonce of the exact AEAD nonce size and never reuse a nonce with a key.
- Do not design custom encryption, token, signature, or password schemes.

```go
func encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AEAD: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}
```

Use `crypto/rand.Text()` when its fixed 128-bit random text token meets the protocol;
otherwise generate bytes and encode them without modulo bias.

## HTTP and Network Services

- Set server read-header, read, write, and idle timeouts based on endpoint behavior.
- Bind local-only services explicitly to loopback when remote access is not required.
- Configure TLS minimum versions from compatibility and threat requirements; avoid
  obsolete cipher or header cargo cults.
- Set `X-Content-Type-Options: nosniff` and an explicit content type for responses.
- Use a deliberate Content Security Policy for browser content when applicable.
- Configure CORS with an exact allowlist. CORS is not authorization.
- Apply rate or concurrency limits at abuse-prone endpoints and ensure limiter keys
  cannot be forged through untrusted forwarding headers.

## Tests and Review

Cover applicable cases:

- empty, malformed, boundary, oversized, and adversarial input
- missing, invalid, expired, and wrong-scope credentials
- cross-resource authorization attempts
- SQL metacharacters and command arguments treated as data
- traversal, absolute path, sibling-prefix, and symlink cases
- nonce/random source failure where injectable
- secret redaction in errors and logs
- concurrent access under `go test -race`

Before handoff, search the diff for embedded secrets, ignored errors, unsafe shell
construction, weak randomness, unauthenticated encryption, broad logging, and missing
authorization checks. Report residual risks rather than claiming absolute security.

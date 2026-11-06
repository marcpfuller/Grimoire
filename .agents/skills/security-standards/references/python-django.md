# Python and Django Security Guidance

Use the project's supported framework APIs and review Django's maintained
[security guidance](https://docs.djangoproject.com/en/5.2/topics/security/) for the
deployed version.

## Inputs and Authorization

- Validate request and domain data through the project's established Django forms
  or DRF serializers. Treat raw request data, serializer `initial_data`, session
  values, and decoded JSON as untrusted.
- Apply request and upload limits at the earliest controllable boundary, including
  the reverse proxy when applicable. Do not read an unbounded upload into memory.
- Use Django authentication primitives and enforce object-level authorization for
  the requested resource. Authentication and model lookup do not imply permission.
- Keep CSRF protection enabled for cookie-authenticated state changes. CORS does not
  replace CSRF or authorization.

Use Django's password hashers and token/signing facilities when they fit the
protocol. Use [`secrets`](https://docs.python.org/3/library/secrets.html), including
`compare_digest`, for security tokens and comparisons rather than `random`.

## ORM, Commands, and Files

- Prefer the Django ORM. Pass parameters separately to raw SQL and allowlist dynamic
  identifiers; never build SQL with string interpolation.
- Use [`subprocess`](https://docs.python.org/3/library/subprocess.html) with an
  argument sequence, `shell=False`, a timeout, and a deliberately scoped environment.
- Use Django storage APIs for uploaded files. Treat names and client MIME types as
  untrusted, enforce size limits, and generate server-controlled storage names.
- For local paths, resolve containment against an intended root and account for
  symlink and time-of-check/time-of-use risks. Use `tempfile` for temporary paths.

## Secrets, Errors, and HTTP

- Read secrets through the project's settings/environment boundary and keep them out
  of reprs, validation errors, debug pages, task arguments, and structured logs.
- Map expected domain failures to stable external responses. Preserve internal
  exception context in protected diagnostics without returning tracebacks or object
  dumps.
- Configure `ALLOWED_HOSTS`, trusted proxy behavior, secure cookies, HTTPS redirect,
  HSTS, content sniffing protection, and referrer policy for the actual deployment.
- Use Django `transaction.on_commit()` when an external task should be scheduled only
  after a database state change commits.

Do not design cryptographic formats. Prefer Django signing for framework-suitable
signed values or a maintained high-level cryptography library for encryption.

Apply the Python/Django profile from `$testing-standards`. Include cross-tenant or
cross-resource authorization, malformed and oversized input, upload traversal,
unsafe redirects, secret redaction, and transaction/task ordering where applicable.

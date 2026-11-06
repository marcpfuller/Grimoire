---
name: security-standards
description: Threat-model and secure boundaries involving untrusted input, authentication, authorization, secrets, cryptography, SQL, HTTP, filesystem access, command execution, or sensitive logging. Use for security-sensitive implementation and review, not for unrelated internal refactors.
---

# Security Standards

Apply controls based on the actual trust boundary and deployment model. Do not add
security theater or copy middleware without establishing the threat it addresses.

## Select Language Guidance

Read only the profile matching the changed boundary:

- Go: [references/go.md](references/go.md)
- Python or Django: [references/python-django.md](references/python-django.md)

For other ecosystems, use maintained platform primitives and authoritative security
guidance rather than translating code examples literally.

## Threat Model First

Identify:

- attacker-controlled inputs and maximum accepted sizes
- the authoritative identity and authorization decision
- secrets or sensitive data in memory, storage, errors, and logs
- filesystem, database, network, and process capabilities reached by the input
- denial-of-service, replay, race, and confused-deputy risks
- the external error contract and internal diagnostic path

For security-critical work, record controls that are not applicable and why.

## Input Validation

- Enforce size limits before expensive decoding, allocation, regex work,
  decompression, or database access.
- Validate type, range, format, enumeration membership, and cross-field invariants.
- Normalize only when the protocol defines equivalent forms; validate the normalized
  value used by the operation.
- Reject unknown fields when silent acceptance could hide a client error or bypass
  policy.
- Use bounded work and request-scoped deadlines for network input.

Validate transport shape and domain rules at their respective boundaries.

## Authentication and Authorization

- Authenticate credentials before trusting identity claims.
- Authorize every protected operation against the requested resource.
- Deny by default and avoid distinguishable external failures when they would leak
  account or resource existence.
- Validate token algorithm, signature, issuer, audience, expiry, and not-before as
  required. Do not trust an algorithm selected only by token metadata.
- Use the platform's established password hashing and verification APIs.
- Compare opaque secret tokens in constant time after checking expected lengths.

Typed identity state is authoritative only when trusted authentication middleware or
an equivalent boundary established its provenance.

## Secrets, Errors, and Logging

- Load secrets from an approved secret source or environment; never commit them.
- Fail safely when required configuration is absent.
- Do not log passwords, tokens, cookies, authorization headers, keys, or complete
  sensitive payloads.
- Prefer allowlisted structured fields over broad object dumps.
- Return stable generic errors across untrusted boundaries while retaining useful
  internal diagnostics without exposing them.

## SQL and Process Execution

- Use parameterized SQL for values. Select identifiers from a fixed allowlist rather
  than concatenating user input.
- Check transaction, iteration, scan, commit, and rollback failures as applicable.
- Avoid invoking a shell. Pass executable and arguments separately with a deadline.
- Never interpolate untrusted input into a shell command string.
- Treat captured user commands as data and prevent accidental re-execution.

Do not include sensitive process output in returned errors or logs.

## Filesystem Access

- Establish containment against traversal, sibling-prefix, and symlink escape; a
  string prefix check is not containment.
- Create secret-bearing files with restrictive permissions from the start.
- Use unpredictable temporary names and cleanup tied to resource lifetime.
- Limit uploads before reading, inspect content when relevant, and do not trust
  filename extensions or client MIME declarations.

## Cryptography and Randomness

- Use the ecosystem's cryptographically secure randomness source for tokens, nonces,
  and security identifiers.
- Use authenticated encryption or a maintained high-level cryptographic API.
- Never reuse a nonce with a key or design a custom encryption, signature, token, or
  password scheme.

## HTTP and Network Services

- Configure timeouts, request limits, and concurrency limits for endpoint behavior.
- Bind local-only services explicitly to loopback.
- Use deliberate TLS, content-type, browser-policy, cookie, and origin settings.
- Treat CORS as a browser policy, not authorization.
- Do not trust forwarding headers unless a controlled proxy establishes them.

## Tests and Review

Cover applicable malformed, boundary, oversized, adversarial, authentication,
authorization, injection, traversal, redaction, and concurrency cases. Before
handoff, search the diff for embedded secrets, ignored failures, unsafe shell use,
weak randomness, unauthenticated encryption, broad logging, and missing resource
authorization. Report residual risk rather than claiming absolute security.

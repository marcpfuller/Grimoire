---
name: typed-state-first
description: Find and use authoritative typed state before reading values from loose maps, context storage, serialized blobs, metadata, or parameter bags. Use when loosely typed access controls behavior or when a proposed helper may duplicate an existing typed field or API; do not use for ordinary typed code.
---

# Typed State First

Prefer authoritative typed state over loosely typed containers.

## Select Language Guidance

Read only the matching profile when concrete examples or ecosystem-specific type
boundaries are needed:

- Go: [references/go.md](references/go.md)
- Python or Django: [references/python-django.md](references/python-django.md)

The procedure applies to other languages without forcing either profile.

## Procedure

1. Read the complete typed definitions involved in the current path, including
   relevant nested, inherited, embedded, generated, and framework-provided types.
2. Check existing fields, properties, and methods for the value before reading a
   map, context store, serialized blob, query parameter, or metadata container.
3. Establish which source is authoritative. A typed field is not automatically
   trustworthy if it was populated from the same untrusted input.
4. Search for existing package or module APIs that provide the value or perform the
   same lookup, then inspect their callers.
5. Reuse or adapt an existing API when it already provides the required data. A
   broader result or less convenient shape alone does not justify a parallel helper.
6. Add a narrowly typed API only when the existing path cannot be reused safely or
   correctly. State that concrete limitation before implementation.

For raw external maps, JSON objects, and metadata, first locate the project's
validated representation. If none exists, validate the runtime shape before reading
keys or values. A type annotation, `cast()`, or a check performed only by a separate
method does not prove that the current code path has a mapping with the expected
keys.

When an operation depends only on a scalar, expose a focused typed function and let
larger objects delegate to it instead of constructing partial objects merely to call
a method.

For values controlling authorization, SQL, filesystem paths, command execution,
network destinations, or secret selection, validate provenance as well as type. Do
not silently fall back to untrusted metadata when authoritative typed state is
absent.

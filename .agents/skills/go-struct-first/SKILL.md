---
name: go-struct-first
description: Check relevant Go structs and typed APIs before reading authoritative values from map[string]any, context values, JSON blobs, metadata maps, or other loosely typed containers. Use for loosely typed access or when proposing a helper that may duplicate an existing typed field or method; do not use for general Go work.
---

# Go Struct First

Prefer authoritative typed state over loosely typed containers.

## Procedure

1. Read the complete definitions of the structs involved in the current path,
   including relevant embedded and nested structs.
2. Check existing fields and methods for the value before reading a map, context
   value, serialized blob, or query parameter.
3. Confirm which source is authoritative. A typed field is not automatically safe
   if it was populated from the same untrusted value.
4. If no typed source exists, check for an existing package function before adding
   a helper.
5. Add a narrowly typed API when the same loose access recurs or controls a
   security-sensitive decision.

Prefer:

```go
path := input.Project.Root
```

over:

```go
path, _ := input.Metadata["project_root"].(string)
```

If an operation depends only on a scalar, expose a package function and let methods
delegate to it rather than constructing partial structs:

```go
func CommandPath(id int64) string { return fmt.Sprintf("/commands/%d", id) }
func (c Command) Path() string     { return CommandPath(c.ID) }
```

For values controlling authorization, SQL, filesystem paths, command execution, or
secret selection, validate provenance as well as type. Do not silently fall back to
untrusted metadata when an authoritative typed value is absent.

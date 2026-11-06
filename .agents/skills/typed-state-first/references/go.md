# Go Typed-State Guidance

Read complete struct definitions, including embedded and nested structs, and inspect
typed methods before using `map[string]any`, context values, JSON blobs, or metadata.

Prefer:

```go
path := input.Project.Root
```

over:

```go
path, _ := input.Metadata["project_root"].(string)
```

If an operation depends only on a scalar, expose a package function and let methods
delegate to it:

```go
func CommandPath(id int64) string { return fmt.Sprintf("/commands/%d", id) }
func (c Command) Path() string     { return CommandPath(c.ID) }
```

Use checked type assertions when loose input is unavoidable. A private context-key
type prevents collisions but does not prove the stored value's provenance.

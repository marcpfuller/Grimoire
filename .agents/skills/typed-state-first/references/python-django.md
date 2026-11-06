# Python and Django Typed-State Guidance

Inspect dataclasses, typed domain objects, Django models, forms, serializers,
`TypedDict` definitions, and generated client types before reading a
`dict[str, Any]`, session value, JSON object, or metadata bag.

Prefer:

```python
root = command.project.root
```

over:

```python
root = command.metadata.get("project_root")
```

Important boundaries:

- A `TypedDict` helps static checking but does not validate a dictionary at runtime.
- DRF serializer data becomes authoritative only after successful validation; use
  `validated_data`, not raw `initial_data`, for the operation.
- A Django model field is typed state, but authorization still depends on the
  authenticated actor and requested resource, not merely an ID copied from input.
- Parse external JSON into the project's established validated representation at
  the boundary. Do not add Pydantic when the project uses forms, serializers, or
  dataclasses unless the user settles that architectural choice.
- Use a dataclass for a meaningful command or value object, not to disguise a loose
  bag behind typed attribute names.

When an optional field can be absent, represent and handle that absence explicitly.
Do not use `cast()` or `# type: ignore` as runtime validation.

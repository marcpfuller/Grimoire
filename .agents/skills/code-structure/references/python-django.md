# Python and Django Boundary Guidance

Use these rules after the shared workflow establishes that a Python module or
package boundary should change. Follow the repository's established conventions
before introducing a new architectural pattern.

Google's [Python Style Guide](https://google.github.io/styleguide/pyguide.html)
informs the API-shape rules below: use clear package imports, avoid mutable global
state, type public APIs where useful, treat circular dependencies as design smells,
and keep related classes and functions together. It is a style guide, not a mandate
for a particular application architecture.

## Module and API Shape

- Put behavior in the package or Django app that owns it. Prefer a behavior-named
  module over a generic `utils.py`, `helpers.py`, or catch-all `services.py`.
- Start with a module. Create a subpackage only when several cohesive modules need
  a shared public boundary.
- Prefer functions for stateless behavior. Use a class when state, lifecycle, or a
  real interchangeable strategy makes the ownership clearer.
- Accept explicit inputs and return useful values. Use keyword-only parameters when
  they make workflow calls harder to misuse.
- Type public and cross-module APIs. Add a dataclass for a real value object or
  command, not merely to wrap every function's arguments.
- Introduce `Protocol` or another abstraction only at a consumer boundary with a
  demonstrated need for substitution. Do not add one solely to make mocking easy.
- Raise built-in exceptions for programming errors and focused domain exceptions
  for expected business failures. Catch narrowly at an entry-point boundary; do
  not use `assert` for input validation or swallow exceptions.
- Avoid mutable module globals, import-time I/O, and hidden registration side
  effects. Resolve import cycles by correcting ownership rather than adding local
  imports everywhere.

## Django Ownership

Keep framework entry points thin enough that the same use case can be called from
another legitimate entry point without reproducing policy.

| Component | Owns |
| --- | --- |
| View, serializer, or form | HTTP/input parsing, authentication context, representation, and boundary validation |
| Workflow function or use-case object | Authorization policy, state transitions, ordering, and transaction boundary |
| Model method | Invariants and behavior local to one model instance |
| QuerySet or manager | Reusable query and persistence mechanics |
| Task or management command | Delivery, scheduling, retries, and translation into a workflow call |
| Signal | A small unavoidable reaction; not a hidden primary workflow |
| Integration client | Third-party protocol details and translation of external failures |

Place `transaction.atomic()` around the use case that owns the consistent state
change. Avoid external network calls inside a database transaction when they can be
performed before it or reliably scheduled after commit.

```python
import dataclasses

from django.db import transaction

from ghostwriter.reporting import models
from ghostwriter.reporting import policies


@dataclasses.dataclass(frozen=True)
class PublishReportCommand:
    report_id: int
    actor_id: int


@transaction.atomic
def publish_report(command: PublishReportCommand) -> models.Report:
    report = models.Report.objects.select_for_update().get(pk=command.report_id)
    policies.ensure_can_publish(report=report, actor_id=command.actor_id)
    report.mark_published()
    report.save()
    return report
```

The view, task, or command constructs the typed command and maps domain failures to
its transport. The workflow owns authorization, ordering, and atomicity. The model
owns its local state transition, while the QuerySet owns locking and retrieval.
These are ownership examples, not required filenames or abstractions.

## Refactoring and Verification

- Preserve the project's current test style and runner.
- Lock down behavior at the narrowest stable seam before moving it.
- Test the workflow directly for policy and state transitions; keep transport tests
  for request/response mapping and integration tests for ORM behavior.
- Run focused tests during migration, then the applicable Django/Python suite,
  formatter, linter, and type checker.
- Check migrations when model state or fields change, and query counts when moving
  ORM access could affect loading behavior.

Do not reorganize a module because of size alone. Require concrete mixed
responsibilities, duplicated behavior, an import cycle, or a missing stable seam.

# Python and Django Testing Guidance

Match the repository's current framework and style. Do not introduce pytest into a
`unittest` suite, or rewrite Django `TestCase` tests as pytest tests, merely for
preference. The [Django testing documentation](https://docs.djangoproject.com/en/5.2/topics/testing/)
is the authority for Django test isolation and database behavior.

## Test Shape

Use `subTest` or the configured framework's parameterization when cases share setup
and assertions. Keep a direct test when there is only one behavior.

```python
class ValidateNameTests(SimpleTestCase):
    def test_rejects_invalid_names(self):
        cases = {
            "empty": "",
            "too long": "a" * 256,
        }

        for name, value in cases.items():
            with self.subTest(name=name):
                with self.assertRaises(ValidationError):
                    validate_name(value)
```

Prefer `assertRaises`, `assertEqual`, or the repository's established assertion API
over manually catching exceptions. Assert stable domain or framework exceptions;
avoid exact internal messages unless the text is part of the external contract.

## Django Boundaries

- Use `SimpleTestCase` when no database access is needed.
- Use `TestCase` for ordinary ORM behavior with test isolation.
- Use `TransactionTestCase` only when commits, rollbacks, locking, or transaction
  boundaries are themselves under test.
- Test workflow policy directly. Use Django's test client or request factory for
  request/response mapping, middleware, permissions, and rendering.
- Use query-count assertions for changes that could introduce N+1 queries.
- Test migrations with the repository's established migration-test pattern rather
  than importing historical models from current application code.

Patch the symbol where the code under test looks it up. Use `autospec` or equivalent
when it catches interface drift, and configure only behavior relevant to the test.
Do not mock QuerySets so deeply that the ORM behavior under test disappears.

For asynchronous tasks, test the workflow synchronously where possible and keep a
smaller integration test for queue delivery, retry policy, and serialization.

## Commands

Use repository instructions and its environment boundary. Typical commands include:

```bash
python manage.py test path.to.tests.TestCase.test_behavior
python manage.py test path.to.app
coverage run manage.py test
coverage report
```

When the repository requires Docker, run these through its documented Compose
service. Run configured formatters, linters, and type checks after focused tests.

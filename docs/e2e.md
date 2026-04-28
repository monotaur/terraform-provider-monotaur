# End-to-End Tests

End-to-end (E2E) tests verify the provider against a live Monotaur API instance.
They are acceptance tests gated on `TF_ACC=1` and are run via `gotestsum` for
structured output and JUnit XML reporting.

## Prerequisites

### Go version

Go 1.21 or later is required. Check with:

```bash
go version
```

### Build tools

- `make` — all E2E targets are exposed as Makefile rules
- `gotestsum` — fetched automatically via `go run gotest.tools/gotestsum`; no manual install needed

### Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MONOTAUR_ENDPOINT` | yes | Base URL of the Monotaur API, e.g. `https://api.staging.monotaur.io` |
| `MONOTAUR_API_KEY` | yes | API key with write access (create, update, delete) to the staging environment |
| `MONOTAUR_LOG_LEVEL` | no | Provider log verbosity during tests, e.g. `DEBUG` |

Both required variables must be set before running any E2E target. The
preflight script (`scripts/e2e-preflight.sh`) validates them and aborts with a
one-line error message if either is missing.

### Staging API access

Tests run against the staging Monotaur environment. You need:

- Network access to `MONOTAUR_ENDPOINT`
- An API key that has permission to create and delete all resource types tested
  by the suite

Contact your team to obtain staging credentials if you do not already have them.

## Running the suite

### Full suite

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-key>

make e2e
```

This runs the preflight check, then executes all tests under `./internal/provider/...`
with a 30-minute timeout. Output is written to `e2e-results/`.

### Single test

```bash
make e2e-one TEST=TestAccMonotaurLabel_basic
```

Use this for targeted iteration. The timeout is 10 minutes for single-test runs.

### Intentional re-runs

Use `make e2e-one` for any retry — it is always safe to re-run because the
sweeper runs automatically as part of `make e2e` before the suite starts. For
`make e2e-one`, you may want to run `make e2e-sweep` first if you suspect stale
state from a previous interrupted run.

## Reading the output

### `e2e-results/summary.md`

This is the primary output file. After `make e2e` completes, read it to
determine whether the suite passed or failed.

**Claude: read `e2e-results/summary.md` after `make e2e`; exit 0 + `Result: PASS` line means success.**

The file contains:

- `Result: PASS` — all tests passed, or `Result: FAIL` — one or more tests failed
- Total test count, pass count, and fail count
- Names of any failing tests

Example passing summary:

```
Result: PASS
Tests: 12 passed, 0 failed, 0 skipped
```

Example failing summary:

```
Result: FAIL
Tests: 11 passed, 1 failed, 0 skipped

Failed tests:
- TestAccMonotaurLabel_update
```

The summary is written by the `e2e-reporter` tool (see issue #63). Until that
tool ships, `summary.md` will not be present; rely on the `make e2e` exit code
and `junit.xml` instead.

### `e2e-results/junit.xml`

JUnit XML report suitable for upload to CI artifact stores (GitHub Actions,
GitLab CI, etc.). Every test case is listed with its pass/fail status and
duration.

### `e2e-results/raw.jsonl`

Raw `go test -json` event stream — one JSON object per line. Use this for
verbose debugging when you need full log output, API responses, or Terraform
plan diffs.

```bash
# Show only FAIL events
grep '"Action":"fail"' e2e-results/raw.jsonl | jq .

# Show all output for a specific test
grep 'TestAccMonotaurLabel_update' e2e-results/raw.jsonl | jq -r '.Output // empty'
```

All three files are git-ignored and re-created on every run.

## Resource naming

Tests use `internal/acctest.Name(resourceType, n)` to generate collision-free
resource names scoped to the current process run:

```
tfe2e-<6-char-hex-prefix>-<resourceType>-<n>
```

Example: `tfe2e-a3f9c2-label-1`

The prefix is generated once per process (via `acctest.RunPrefix()`) so all
resources created in a single run share the same prefix. This serves two
purposes:

1. **Safe cleanup** — the sweeper can delete exactly the resources from a
   specific run without touching unrelated staging data.
2. **Collision avoidance** — concurrent CI runs use distinct prefixes and
   cannot interfere with each other's resources.

When writing a new test, always use `acctest.Name(...)` for resource names.
Never use hard-coded strings.

## Sweeper behavior

`make e2e-sweep` deletes all resources whose names match `tfe2e-*` in the
staging environment.

Key facts:

- The sweeper runs automatically before `make e2e` to clear any leftover state
  from previous interrupted runs.
- Nightly CI also runs `make e2e-sweep` as a standalone job to keep staging tidy.
- Sweeper failures are logged and reported but are **non-fatal** for the test
  suite — the suite continues even if the sweep fails.
- Run `make e2e-sweep` manually any time you want to clean up staging without
  running the full suite.

## Debugging a failing test

1. **Identify the failing test name** from `e2e-results/summary.md` (or
   `junit.xml`).

2. **Reproduce in isolation:**
   ```bash
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

3. **Inspect raw output** for API error messages:
   ```bash
   grep 'TestAccMonotaurLabel_update' e2e-results/raw.jsonl | jq -r '.Output // empty'
   ```

4. **Check staging state.** If a previous run left resources behind, the test
   may fail because a resource already exists. Run the sweeper first:
   ```bash
   make e2e-sweep
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

5. **Enable debug logging** for more verbose provider output:
   ```bash
   export MONOTAUR_LOG_LEVEL=DEBUG
   make e2e-one TEST=TestAccMonotaurLabel_update
   ```

## Common errors

### `Error: MONOTAUR_ENDPOINT not set — see docs/e2e.md`

The preflight script rejected the run. Export the required variables before
invoking any E2E target:

```bash
export MONOTAUR_ENDPOINT=https://api.staging.monotaur.io
export MONOTAUR_API_KEY=<your-key>
```

### `Error: MONOTAUR_API_KEY not set — see docs/e2e.md`

Same as above — the `MONOTAUR_API_KEY` variable is missing.

### `401 Unauthorized`

The API key is invalid, expired, or does not have write access to the staging
environment. Verify the key is correct and has not been rotated. Obtain a new
key from your team if needed.

### Rate limit errors

If the suite triggers API rate limits, the affected tests will fail with HTTP
429 responses. Options:

- Reduce parallelism: the suite currently runs tests sequentially within a
  package; no additional flags are needed for most cases.
- If running multiple suite invocations back-to-back, add a short pause between
  runs.
- Run only the failing test with `make e2e-one TEST=<name>` rather than the
  full suite.

### Leftover state / pre-existing resource name conflicts

If `make e2e` was interrupted (Ctrl-C, OOM, timeout), resources from the
aborted run may still exist in staging. The next run will generate a new
process prefix, so name collisions are rare — but if you see errors like
`resource already exists` or unexpected plan diffs, run the sweeper first:

```bash
make e2e-sweep
make e2e
```

### Tests are skipped unexpectedly

`TF_ACC=1` is injected automatically by all E2E Makefile targets. If you are
running `go test` directly, set it manually:

```bash
TF_ACC=1 go test -run TestAccMonotaurLabel_basic -v ./internal/provider/...
```

### Timeout exceeded

The full suite uses a 30-minute timeout. Individual test runs via `make e2e-one`
use 10 minutes. If a test legitimately needs more time (e.g. a slow API
operation), adjust the timeout with `TESTARGS`:

```bash
TESTARGS="-timeout 20m" make e2e-one TEST=TestAccMonotaurLabel_basic
```
